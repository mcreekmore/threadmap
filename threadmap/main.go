package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	dockerSocket  = "/run/docker.sock"
	ingressPeer   = "172.30.32.2" // Supervisor's ingress gateway, which enforces the HA login.
	listenAddr    = ":8099"
	commandLimit  = 15 * time.Second
	scanLimit     = 90 * time.Second
	protectionMsg = "can't reach Docker. Turn Protection mode off for this add-on, then restart it"
)

//go:embed all:web/dist
var webFiles embed.FS

// otctl runs one ot-ctl command in the border router.
type otctl func(ctx context.Context, args ...string) (string, error)

// scan only issues read-only commands. Never add `dataset` here: its output contains the network key.
func scan(ctx context.Context, run otctl) (meshMap, error) {
	var warnings []string
	warnf := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	out, err := run(ctx, "meshdiag", "topology", "children")
	routers := parseTopology(out)
	if err != nil || len(routers) == 0 {
		warnf("Couldn't read child tables, so some children may be missing.")
		out, err = run(ctx, "meshdiag", "topology")
		if err != nil {
			return meshMap{}, fmt.Errorf("meshdiag topology: %w", err)
		}
		routers = parseTopology(out)
	}
	if len(routers) == 0 {
		return meshMap{}, errors.New("meshdiag topology returned no routers")
	}

	var services []service
	if out, err := run(ctx, "srp", "server", "service"); err != nil {
		warnf("Couldn't read device names: %v", err)
	} else {
		services = parseServices(out)
	}

	readCache := func() map[netip.Addr]uint16 {
		out, err := run(ctx, "eidcache")
		if err != nil {
			warnf("Couldn't read the address cache: %v", err)
			return map[netip.Addr]uint16{}
		}
		return parseEIDCache(out)
	}
	cache := readCache()
	if targets := pingTargets(routers, services, cache); len(targets) > 0 {
		for _, addr := range targets {
			run(ctx, "ping", addr.String()) // A sleepy device may not answer; buildMap warns if it stays unlocated.
		}
		cache = readCache()
	}

	m := buildMap(routers, services, cache)
	m.Warnings = append(warnings, m.Warnings...)
	if m.Warnings == nil {
		m.Warnings = []string{}
	}
	return m, nil
}

type docker struct {
	client *http.Client
}

func newDocker() *docker {
	return &docker{client: &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", dockerSocket)
		},
	}}}
}

func (d *docker) call(ctx context.Context, method, path string, body, out any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, errors.New(protectionMsg)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker %s: %s: %s", path, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return data, json.Unmarshal(data, out)
	}
	return data, nil
}

func (d *docker) findBorderRouter(ctx context.Context) (string, error) {
	var containers []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
	}
	if _, err := d.call(ctx, http.MethodGet, "/containers/json", nil, &containers); err != nil {
		return "", err
	}
	var matches []string
	for _, c := range containers {
		if strings.Contains(strings.Join(c.Names, " "), "openthread") {
			matches = append(matches, c.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", errors.New("no running container matches openthread. Is the OpenThread Border Router add-on started?")
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("found %d containers matching openthread, expected 1", len(matches))
}

// otctl returns a runner bound to one container. With Tty set, Docker sends plain output with CRLF line endings.
func (d *docker) otctl(container string) otctl {
	return func(ctx context.Context, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, commandLimit)
		defer cancel()
		var created struct {
			ID string `json:"Id"`
		}
		cmd := append([]string{"ot-ctl"}, args...)
		spec := map[string]any{"AttachStdout": true, "AttachStderr": true, "Tty": true, "Cmd": cmd}
		if _, err := d.call(ctx, http.MethodPost, "/containers/"+container+"/exec", spec, &created); err != nil {
			return "", err
		}
		data, err := d.call(ctx, http.MethodPost, "/exec/"+created.ID+"/start", map[string]any{"Detach": false, "Tty": true}, nil)
		if err != nil {
			return "", err
		}
		out := strings.ReplaceAll(string(data), "\r", "")
		var inspect struct {
			ExitCode int `json:"ExitCode"`
		}
		if _, err := d.call(ctx, http.MethodGet, "/exec/"+created.ID+"/json", nil, &inspect); err != nil {
			return "", err
		}
		if inspect.ExitCode != 0 {
			return out, fmt.Errorf("exit code %d: %s", inspect.ExitCode, strings.TrimSpace(out))
		}
		return out, nil
	}
}

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

func routes(site fs.FS, find func(context.Context) (otctl, error)) http.Handler {
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
		run, err := find(ctx)
		var result meshMap
		if err == nil {
			result, err = scan(ctx, run)
		}
		if err != nil {
			log.Printf("scan failed: %v", err)
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
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
	d := newDocker()
	find := func(ctx context.Context) (otctl, error) {
		id, err := d.findBorderRouter(ctx)
		if err != nil {
			return nil, err
		}
		return d.otctl(id), nil
	}
	log.Printf("listening on %s", listenAddr)
	go func() {
		log.Fatal(http.ListenAndServe(listenAddr, onlyFromIngress(ingressPeer, routes(site, find))))
	}()
	// Exit cleanly on stop; otherwise HA shows the stopped add-on as failed.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	<-stop
}
