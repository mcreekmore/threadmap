package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeRun answers from canned output and records the commands it was asked to run.
func fakeRun(outputs map[string]string, calls *[]string) otctl {
	return func(_ context.Context, args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		*calls = append(*calls, cmd)
		if out, ok := outputs[cmd]; ok {
			return out, nil
		}
		return "", errors.New("failed")
	}
}

func fixtures(t *testing.T) map[string]string {
	return map[string]string{
		"meshdiag topology":  fixture(t, "meshdiag_topology.txt"),
		"srp server service": fixture(t, "srp_server_service.txt"),
		"eidcache":           fixture(t, "eidcache.txt"),
	}
}

func TestScanFixtures(t *testing.T) {
	var calls []string
	m, err := scan(context.Background(), fakeRun(fixtures(t), &calls))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Nodes) != 2 || len(m.Links) != 1 || len(m.Warnings) != 0 {
		t.Fatalf("unexpected map: %+v", m)
	}
	humidifier, border := m.Nodes[0], m.Nodes[1]
	if humidifier.Label != "Test Humidifier 0001" || humidifier.RLOC != "0xec00" || humidifier.MAC != "1111222233334444" {
		t.Errorf("humidifier = %+v", humidifier)
	}
	if border.Label != "Border router" || !border.Leader || !border.Border || border.MAC != "aaaabbbbccccdddd" {
		t.Errorf("border = %+v", border)
	}
	if l := m.Links[0]; l.A != "0xec00" || l.B != "0xf400" || l.Quality == nil || *l.Quality != 3 {
		t.Errorf("link = %+v", l)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "ping") {
			t.Errorf("pinged a device the cache already located: %s", c)
		}
	}
}

func TestScanPingsOnlyOnMiss(t *testing.T) {
	var calls []string
	outputs := fixtures(t)
	outputs["eidcache"] = "Done\n"
	outputs["ping fd00:db8:1:1::1"] = "Done\n"
	m, err := scan(context.Background(), fakeRun(outputs, &calls))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(calls, "|")
	want := "meshdiag topology|srp server service|eidcache|ping fd00:db8:1:1::1|eidcache"
	if got != want {
		t.Errorf("calls = %s", got)
	}
	// Still unlocated after the ping: falls back to the MAC and warns.
	if m.Nodes[0].Label != "1111222233334444" || len(m.Warnings) != 1 {
		t.Errorf("unexpected map: %+v %v", m.Nodes[0], m.Warnings)
	}
}

func TestScanErrors(t *testing.T) {
	var calls []string
	if _, err := scan(context.Background(), fakeRun(map[string]string{}, &calls)); err == nil {
		t.Error("topology failure should be fatal")
	}
	// Names and cache failing is not fatal.
	m, err := scan(context.Background(), fakeRun(map[string]string{"meshdiag topology": fixture(t, "meshdiag_topology.txt")}, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if m.Nodes[0].Label != "1111222233334444" || len(m.Warnings) != 2 {
		t.Errorf("unexpected map: %+v %v", m.Nodes[0], m.Warnings)
	}
}

func TestScanNeverRequestsDataset(t *testing.T) {
	var calls []string
	outputs := fixtures(t)
	outputs["eidcache"] = "Done\n"
	scan(context.Background(), fakeRun(outputs, &calls))
	for _, c := range calls {
		if strings.Contains(c, "dataset") {
			t.Errorf("ran %q", c)
		}
	}
}

func TestParseTopologyVariants(t *testing.T) {
	routers := parseTopology("junk\nid:1 rloc16:0x0400 ext-addr:aa\n    0-links:{ 2 }\n    2-links:{ 3 4 }\n    1-links:{ }\nsomething new\n")
	if len(routers) != 1 {
		t.Fatalf("routers = %+v", routers)
	}
	if l := routers[0].linksTo; l[2] != 0 || l[3] != 2 || l[4] != 2 || len(l) != 3 {
		t.Errorf("links = %v", l)
	}
}

func TestParseServices(t *testing.T) {
	out := "Gone Device._hap._udp.default.service.arpa.\n    deleted: true\n    addresses: [fd00::1]\n" +
		"Two Addrs._matter._tcp.default.service.arpa.\n    deleted: false\n    addresses: [fd00::2, fd00::3, not-an-ip]\nDone\n"
	s := parseServices(out)
	if len(s) != 1 || s[0].name != "Two Addrs" || len(s[0].addrs) != 2 {
		t.Errorf("services = %+v", s)
	}
}

func TestParseEIDCacheSkipsPendingLookups(t *testing.T) {
	c := parseEIDCache("fd00::1 fffe retry canEvict=1\nfd00::2 0xEC01 cache canEvict=1\nnonsense\n")
	if len(c) != 1 || c[netip.MustParseAddr("fd00::2")] != 0xec01 {
		t.Errorf("cache = %v", c)
	}
}

func TestBuildMapChildAndStaleCache(t *testing.T) {
	routers := parseTopology(fixture(t, "meshdiag_topology.txt"))
	a, b := netip.MustParseAddr("fd00::a"), netip.MustParseAddr("fd00::b")
	services := []service{{name: "Sleepy", addrs: []netip.Addr{a}}, {name: "Stale", addrs: []netip.Addr{b}}}
	// 0xec05 is a child of router 59; 0x0400 is router 1, which isn't in the mesh.
	m := buildMap(routers, services, map[netip.Addr]uint16{a: 0xec05, b: 0x0400})

	if len(m.Nodes) != 3 || m.Nodes[1].Label != "Sleepy" || m.Nodes[1].Kind != "child" {
		t.Fatalf("nodes = %+v", m.Nodes)
	}
	var child *link
	for i := range m.Links {
		if m.Links[i].B == "0xec05" {
			child = &m.Links[i]
		}
	}
	if child == nil || child.A != "0xec00" || child.Quality != nil {
		t.Errorf("links = %+v", m.Links)
	}
	if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "Stale") {
		t.Errorf("warnings = %v", m.Warnings)
	}
	if got := pingTargets(routers, services, map[netip.Addr]uint16{a: 0xec05, b: 0x0400}); len(got) != 1 || got[0] != b {
		t.Errorf("ping targets = %v", got)
	}
}

func TestBuildMapKeepsLowerLinkQuality(t *testing.T) {
	routers := parseTopology("id:1 rloc16:0x0400 ext-addr:aa\n    3-links:{ 2 }\nid:2 rloc16:0x0800 ext-addr:bb\n    1-links:{ 1 }\n")
	m := buildMap(routers, nil, nil)
	if len(m.Links) != 1 || *m.Links[0].Quality != 1 {
		t.Errorf("links = %+v", m.Links)
	}
}

// fakeDocker serves the Docker API over TCP, with the client dialing it in place of the socket.
func fakeDocker(t *testing.T, containers string, exitCode int) *docker {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(containers)) })
	mux.HandleFunc("POST /containers/abc/exec", func(w http.ResponseWriter, r *http.Request) {
		var spec struct{ Cmd []string }
		json.NewDecoder(r.Body).Decode(&spec)
		if strings.Join(spec.Cmd, " ") != "ot-ctl eidcache" {
			t.Errorf("cmd = %v", spec.Cmd)
		}
		w.Write([]byte(`{"Id":"e1"}`))
	})
	mux.HandleFunc("POST /exec/e1/start", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("line\r\nDone\r\n")) })
	mux.HandleFunc("GET /exec/e1/json", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{"ExitCode": exitCode})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &docker{client: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", srv.Listener.Addr().String())
		},
	}}}
}

func TestDockerExec(t *testing.T) {
	d := fakeDocker(t, `[{"Id":"abc","Names":["/app_core_openthread_border_router"]},{"Id":"x","Names":["/other"]}]`, 0)
	id, err := d.findBorderRouter(context.Background())
	if err != nil || id != "abc" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
	out, err := d.otctl(id)(context.Background(), "eidcache")
	if err != nil || out != "line\nDone\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
	failing := fakeDocker(t, `[]`, 1)
	if _, err := failing.otctl("abc")(context.Background(), "eidcache"); err == nil {
		t.Error("non-zero exit should be an error")
	}
}

func TestFindBorderRouterNeedsExactlyOne(t *testing.T) {
	for _, containers := range []string{`[]`, `[{"Id":"a","Names":["/openthread1"]},{"Id":"b","Names":["/openthread2"]}]`} {
		if _, err := fakeDocker(t, containers, 0).findBorderRouter(context.Background()); err == nil {
			t.Errorf("%s: expected error", containers)
		}
	}
}

func TestOnlyFromIngress(t *testing.T) {
	h := onlyFromIngress(ingressPeer, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	for addr, want := range map[string]int{"172.30.32.2:1234": 200, "10.0.30.5:1234": 403, "garbage": 403} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: got %d, want %d", addr, rec.Code, want)
		}
	}
}

func TestRoutes(t *testing.T) {
	site := fstest.MapFS{"index.html": {Data: []byte("<title>Thread map</title>")}}
	h := routes(site, func(context.Context) (otctl, error) { return nil, errors.New(protectionMsg) })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Thread map") {
		t.Errorf("index: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/topology", nil))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Protection mode") {
		t.Errorf("topology: %d %s", rec.Code, rec.Body)
	}
}
