package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	ingressPeer = "172.30.32.2"      // Supervisor's ingress gateway, which enforces the HA login.
	listenAddr  = "172.30.32.1:8099" // With host networking, any wider bind would put the page on the LAN.
	restBase    = "http://172.30.32.1:8081"
	scanLimit   = 5 * time.Minute
)

//go:embed all:web/dist
var webFiles embed.FS

// onlyFromIngress rejects anything that didn't come through HA's authenticated ingress proxy.
func onlyFromIngress(peer string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || host != peer {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func routes(site fs.FS, scan func(context.Context) (meshMap, error)) http.Handler {
	var scanning sync.Mutex // Don't let overlapping page loads multiply traffic on the mesh.
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(site))
	mux.HandleFunc("GET /api/topology", func(w http.ResponseWriter, r *http.Request) {
		scanning.Lock()
		defer scanning.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), scanLimit)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		start := time.Now()
		result, err := scan(ctx)
		if err != nil {
			log.Printf("scan failed: %v", err)
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		log.Printf("Scan found %d devices in %v", len(result.Nodes), time.Since(start).Round(time.Millisecond))
		for _, warning := range result.Warnings {
			log.Printf("Warning: %s", warning)
		}
		json.NewEncoder(w).Encode(result)
	})
	return mux
}

func main() {
	site, err := fs.Sub(webFiles, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	border := borderRouter{base: restBase}
	discover := func(ctx context.Context, want <-chan []netip.Addr) []service {
		services := browse(ctx, want)
		named := matchHA(services, fetchHADevices(ctx, coreAPI, os.Getenv("SUPERVISOR_TOKEN")))
		matched := 0
		for _, s := range named {
			if s.ha != nil {
				matched++
			}
		}
		log.Printf("mDNS found %d services, %d of them matched to Home Assistant devices", len(services), matched)
		return named
	}
	scan := func(ctx context.Context) (meshMap, error) { return border.scan(ctx, discover) }
	log.Printf("listening on %s", listenAddr)
	go func() {
		log.Fatal(http.ListenAndServe(listenAddr, onlyFromIngress(ingressPeer, routes(site, scan))))
	}()
	// Exit cleanly on stop; otherwise HA shows the stopped add-on as failed.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	<-stop
}
