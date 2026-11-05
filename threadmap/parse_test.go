package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func init() { taskPoll = time.Millisecond }

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeREST serves the border router's API and records every request.
type fakeREST struct {
	mu          sync.Mutex
	calls       []string
	bodies      []string
	devices     string
	diagnostics string
	status      string // Final status of every task.
	want        []netip.Addr
}

func (f *fakeREST) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	id := fmt.Sprintf("t%d", len(f.bodies))
	switch r.Method + " " + r.URL.Path {
	case "DELETE /api/diagnostics":
		w.WriteHeader(http.StatusNoContent)
	case "POST /api/actions":
		if r.Header.Get("Content-Type") != apiType {
			http.Error(w, "bad type", http.StatusUnsupportedMediaType)
			return
		}
		f.bodies = append(f.bodies, string(body))
		fmt.Fprintf(w, `{"data":[{"id":%q,"attributes":{"status":"pending"}}]}`, id)
	case "GET /api/actions":
		var items []string
		for i := range f.bodies {
			items = append(items, fmt.Sprintf(`{"id":"t%d","attributes":{"status":%q}}`, i, f.status))
		}
		fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(items, ","))
	case "GET /api/devices":
		io.WriteString(w, f.devices)
	case "GET /api/diagnostics":
		io.WriteString(w, f.diagnostics)
	default:
		http.NotFound(w, r)
	}
}

func scanFake(t *testing.T, f *fakeREST, services ...service) (meshMap, error) {
	t.Helper()
	if f.status == "" {
		f.status = "completed"
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return borderRouter{base: srv.URL}.scan(context.Background(), func(_ context.Context, want <-chan []netip.Addr) []service {
		f.want = <-want
		return services
	})
}

func hasTask(bodies []string, kind string) bool {
	return strings.Contains(strings.Join(bodies, ""), kind)
}

func TestScanFixtures(t *testing.T) {
	f := &fakeREST{devices: fixture(t, "devices.json"), diagnostics: fixture(t, "diagnostics.json")}
	humidifier := service{name: "Test Humidifier 0001", addrs: []netip.Addr{netip.MustParseAddr("fd00:db8:1:1::1")}}
	m, err := scanFake(t, f, humidifier)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Nodes) != 2 || len(m.Links) != 1 || len(m.Warnings) != 0 {
		t.Fatalf("unexpected map: %+v", m)
	}
	h, border := m.Nodes[0], m.Nodes[1]
	if h.Label != "Test Humidifier 0001" || h.RLOC != "0xec00" || h.MAC != "1111222233334444" || h.Leader || h.Border {
		t.Errorf("humidifier = %+v", h)
	}
	if border.Label != "Border router" || !border.Leader || !border.Border || border.MAC != "aaaabbbbccccdddd" {
		t.Errorf("border = %+v", border)
	}
	if l := m.Links[0]; l.A != "0xec00" || l.B != "0xf400" || l.Quality != 3 {
		t.Errorf("link = %+v", l)
	}
	// Old results are cleared first, each router gets its own diagnostic task, and the cached devices
	// were complete, so discovery is skipped.
	if f.calls[0] != "DELETE /api/diagnostics" {
		t.Errorf("calls = %v", f.calls)
	}
	if len(f.bodies) != 2 || !strings.Contains(f.bodies[0], "1111222233334444") || !strings.Contains(f.bodies[1], "aaaabbbbccccdddd") || hasTask(f.bodies, `"children"`) {
		t.Errorf("tasks = %v", f.bodies)
	}
	// mDNS only has to name the humidifier; the border router registers no service.
	if !reflect.DeepEqual(f.want, []netip.Addr{netip.MustParseAddr("fd00:db8:1:1::1")}) {
		t.Errorf("want = %v", f.want)
	}
}

func TestScanDiscoversRoutersMissingFromCollection(t *testing.T) {
	diagnostics := strings.Replace(fixture(t, "diagnostics.json"), `"routeData":	[{`, `"routeData":	[{"routeId": 16}, {`, 1)
	f := &fakeREST{devices: fixture(t, "devices.json"), diagnostics: diagnostics}
	m, err := scanFake(t, f)
	if err != nil {
		t.Fatal(err)
	}
	// Both routers, then discovery for router 16, then both routers again.
	if len(f.bodies) != 5 || !strings.Contains(f.bodies[2], "updateDeviceCollectionTask") {
		t.Errorf("tasks = %v", f.bodies)
	}
	if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "0x4000") {
		t.Errorf("warnings = %v", m.Warnings)
	}
}

func TestScanNeverRequestsDataset(t *testing.T) {
	f := &fakeREST{devices: fixture(t, "devices.json"), diagnostics: fixture(t, "diagnostics.json")}
	if _, err := scanFake(t, f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "dataset") {
			t.Errorf("requested %q", c)
		}
	}
}

func TestScanWarnsOnStoppedTasks(t *testing.T) {
	f := &fakeREST{devices: fixture(t, "devices.json"), diagnostics: fixture(t, "diagnostics.json"), status: "stopped"}
	m, err := scanFake(t, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Nodes) != 2 || len(m.Warnings) != 2 {
		t.Errorf("unexpected map: %+v %v", m.Nodes, m.Warnings)
	}
}

func TestScanFailsWithoutRouters(t *testing.T) {
	f := &fakeREST{devices: fixture(t, "devices.json"), diagnostics: `{"data":[]}`}
	if _, err := scanFake(t, f); err == nil {
		t.Error("expected an error")
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := borderRouter{base: url}.scan(context.Background(), func(context.Context, <-chan []netip.Addr) []service { return nil })
	if err == nil || !strings.Contains(err.Error(), "OpenThread Border Router") {
		t.Errorf("err = %v", err)
	}
}

// Child entries are shaped from a real router's answers: the children task stores a second entry per router.
func TestChildFromDiagnostics(t *testing.T) {
	devices := `{"data":[
		{"id":"aa","type":"threadBorderRouter","attributes":{"role":"router","omrIpv6Address":"fd00::a"}},
		{"id":"cc","type":"threadDevice","attributes":{"role":"child","omrIpv6Address":"fd00::c"}}]}`
	diagnostics := `{"data":[
		{"attributes":{"extAddress":"aa","rloc16":"0x0400","route":{"routeData":[]},"childTable":[{"childId":1},{"childId":2}]}},
		{"attributes":{"extAddress":"aa","rloc16":"0x0400","route":{"routeData":[]},
			"children":[{"rloc16":"0x0401","extAddress":"cc","linkMargin":25},{"rloc16":"0x0402","extAddress":"dd","linkMargin":5}],
			"childIpv6Addresses":[{"rloc16":"0x0402","ipv6Addresses":["fdde::1","fd00::d"]}]}}]}`
	f := &fakeREST{devices: devices, diagnostics: diagnostics}
	sensor := service{name: "Sensor", addrs: []netip.Addr{netip.MustParseAddr("fd00::c")}}
	presence := service{name: "Presence", addrs: []netip.Addr{netip.MustParseAddr("fd00::d")}}
	m, err := scanFake(t, f, sensor, presence)
	if err != nil {
		t.Fatal(err)
	}
	// childTable, then children because the table wasn't empty; never the child itself. dd's address came
	// from childIpv6Addresses, so the collection was complete and discovery was skipped.
	if len(f.bodies) != 2 || !strings.Contains(f.bodies[0], "childTable") || !strings.Contains(f.bodies[1], `"children","childIpv6Addresses"`) || hasTask(f.bodies, `"cc"`) {
		t.Errorf("tasks = %v", f.bodies)
	}
	if len(m.Warnings) != 0 {
		t.Errorf("warnings = %v", m.Warnings)
	}
	// One node per router even though it has two entries.
	if len(m.Nodes) != 3 || m.Nodes[1].Label != "Sensor" || m.Nodes[1].Kind != "child" || m.Nodes[2].Label != "Presence" {
		t.Fatalf("nodes = %+v", m.Nodes)
	}
	for i, want := range []link{{A: "0x0400", B: "0x0401", Quality: 3}, {A: "0x0400", B: "0x0402", Quality: 1}} {
		if l := m.Links[i]; l.A != want.A || l.B != want.B || l.Quality != want.Quality || l.Margin == nil || l.AToB != nil {
			t.Errorf("link %d = %+v", i, l)
		}
	}
}

func TestChildWithoutAddressTriggersDiscovery(t *testing.T) {
	devices := `{"data":[{"id":"aa","type":"threadBorderRouter","attributes":{"role":"router","omrIpv6Address":"fd00::a"}}]}`
	diagnostics := `{"data":[{"attributes":{"extAddress":"aa","rloc16":"0x0400","route":{"routeData":[]},"childTable":[{"childId":2}],
		"children":[{"rloc16":"0x0402","extAddress":"dd","linkMargin":5}]}}]}`
	f := &fakeREST{devices: devices, diagnostics: diagnostics}
	m, err := scanFake(t, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.bodies) != 5 || !strings.Contains(f.bodies[2], "updateDeviceCollectionTask") {
		t.Errorf("tasks = %v", f.bodies)
	}
	if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "dd") {
		t.Errorf("warnings = %v", m.Warnings)
	}
}

func TestBuildMapWarnsOnlyForUnlocatedThreadServices(t *testing.T) {
	routers := []router{{id: 1, rloc: 0x0400, mac: "aa", linksTo: map[int]route{}}}
	omrs := map[string]netip.Addr{"aa": netip.MustParseAddr("fd00:1:2:3::a")}
	services := []service{
		{name: "Sleepy", addrs: []netip.Addr{netip.MustParseAddr("fd00:1:2:3::b")}},
		{name: "Wi-Fi Plug", addrs: []netip.Addr{netip.MustParseAddr("fd99::1")}},
	}
	m := buildMap(routers, omrs, services)
	if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "Sleepy") {
		t.Errorf("warnings = %v", m.Warnings)
	}
	if m.Nodes[0].Label != "aa" {
		t.Errorf("label = %q", m.Nodes[0].Label)
	}
	if len(m.Nodes) != 2 || m.Nodes[1].Kind != "missing" || m.Nodes[1].Label != "Sleepy" || m.Nodes[1].RLOC != "" {
		t.Errorf("missing node = %+v", m.Nodes[1:])
	}
}

func TestBuildMapJoinsOneMissingDevicesServices(t *testing.T) {
	routers := []router{{id: 1, rloc: 0x0400, mac: "aa", linksTo: map[int]route{}}}
	omrs := map[string]netip.Addr{"aa": netip.MustParseAddr("fd00:1:2:3::a")}
	sleepy := []netip.Addr{netip.MustParseAddr("fd00:1:2:3::b")}
	m := buildMap(routers, omrs, []service{{name: "Sleepy", addrs: sleepy}, {name: "Sleepy HAP", addrs: sleepy}})
	if len(m.Nodes) != 2 || m.Nodes[1].Label != "Sleepy, Sleepy HAP" || len(m.Warnings) != 1 {
		t.Errorf("nodes = %+v, warnings = %v", m.Nodes, m.Warnings)
	}
}

func TestBuildMapCarriesAddressAndHADevice(t *testing.T) {
	routers := []router{{id: 1, rloc: 0x0400, mac: "aa", linksTo: map[int]route{}}, {id: 2, rloc: 0x0800, mac: "bb", linksTo: map[int]route{}}}
	omrs := map[string]netip.Addr{"aa": netip.MustParseAddr("fd00::a")}
	services := []service{{name: "Plug", addrs: []netip.Addr{netip.MustParseAddr("fd00::a")}, ha: &haDevice{ID: "d1", Name: "Plug"}}}
	out, err := json.Marshal(buildMap(routers, omrs, services).Nodes)
	if err != nil {
		t.Fatal(err)
	}
	// bb has no known address or HA device, so it has neither field.
	want := `[{"rloc":"0x0400","label":"Plug","mac":"aa","kind":"router","omr":"fd00::a","ha":{"id":"d1","name":"Plug"}},{"rloc":"0x0800","label":"bb","mac":"bb","kind":"router"}]`
	if string(out) != want {
		t.Errorf("nodes = %s", out)
	}
}

func TestBuildMapKeepsLowerLinkQuality(t *testing.T) {
	routers := []router{
		{id: 1, rloc: 0x0400, mac: "aa", linksTo: map[int]route{2: {In: 3, Out: 3}, 9: {In: 3, Out: 3}}},
		{id: 2, rloc: 0x0800, mac: "bb", linksTo: map[int]route{1: {In: 1, Out: 3}}},
	}
	m := buildMap(routers, nil, nil)
	// Router 2 hears router 1 at quality 1, though router 1 reported its sending side as 3.
	if len(m.Links) != 1 || m.Links[0].Quality != 1 || *m.Links[0].AToB != 1 || *m.Links[0].BToA != 3 {
		t.Errorf("links = %+v", m.Links)
	}
}

func TestToRouterIgnoresIndirectRoutes(t *testing.T) {
	var d diagnostic
	d.Attributes.Rloc16 = "0xec00"
	d.Attributes.Route.RouteData = []route{{RouteID: 7}, {RouteID: 8, Out: 1, In: 3}}
	r, err := toRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	if r.id != 59 || len(r.linksTo) != 1 || r.linksTo[8] != (route{RouteID: 8, Out: 1, In: 3}) {
		t.Errorf("router = %+v", r)
	}
}

func TestResolved(t *testing.T) {
	addr := netip.MustParseAddr("fd00::1")
	ptr := record{"_hap._udp.local", typePTR, "Lamp._hap._udp.local"}
	srv := record{"Lamp._hap._udp.local", typeSRV, "lamp.local"}
	aaaa := record{"lamp.local", typeAAAA, addr.String()}
	for _, c := range []struct {
		records []record
		want    []netip.Addr
		done    bool
	}{
		{[]record{ptr}, nil, false},
		{[]record{ptr, srv}, nil, false},
		{[]record{ptr, srv, aaaa}, nil, true},
		{[]record{ptr, srv, aaaa}, []netip.Addr{addr, netip.MustParseAddr("fd00::2")}, false},
		{[]record{ptr, srv, aaaa}, []netip.Addr{addr}, true},
	} {
		if got := resolved(c.records, c.want); got != c.done {
			t.Errorf("resolved(%v, %v) = %v", c.records, c.want, got)
		}
	}
}

func TestParseDNS(t *testing.T) {
	// Answers: PTR _hap._udp.local -> "Air 1"._hap._udp.local, SRV -> h.local, AAAA h.local -> fd00::1, and its TXT.
	p := []byte{0, 0, 0x84, 0, 0, 0, 0, 4, 0, 0, 0, 0}
	p = append(p, []byte("\x04_hap\x04_udp\x05local\x00")...) // Offset 12, with "local" at 22.
	p = append(p, 0, 12, 0, 1, 0, 0, 0, 120, 0, 8)
	instance := len(p)
	p = append(p, 5, 'A', 'i', 'r', ' ', '1', 0xC0, 12)
	p = append(p, 0xC0, byte(instance), 0, 33, 0x80, 1, 0, 0, 0, 120, 0, 10, 0, 0, 0, 0, 0x16, 0x33)
	host := len(p)
	p = append(p, 1, 'h', 0xC0, 22)
	p = append(p, 0xC0, byte(host), 0, 28, 0x80, 1, 0, 0, 0, 120, 0, 16)
	addr, _ := hex.DecodeString("fd000000000000000000000000000001")
	p = append(p, addr...)
	txt := "sf=1id=43:3e:01:0f:92:53" // The pairing ID, lower-cased here to check it's normalized.
	p = append(p, 0xC0, byte(instance), 0, 16, 0x80, 1, 0, 0, 0, 120, 0, byte(len(txt)))
	p = append(p, txt...)

	got, err := parseDNS(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []record{
		{"_hap._udp.local", typePTR, "Air 1._hap._udp.local"},
		{"Air 1._hap._udp.local", typeSRV, "h.local"},
		{"h.local", typeAAAA, "fd00::1"},
		{"Air 1._hap._udp.local", typeTXT, "43:3E:01:0F:92:53"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d: got %v, want %v", i, got[i], want[i])
		}
	}

	services := toServices(append(got, record{"Other._meshcop._udp.local", typeSRV, "h.local"}, got[2]))
	if len(services) != 1 || services[0].name != "Air 1" || len(services[0].addrs) != 1 || services[0].hapID != "43:3E:01:0F:92:53" {
		t.Errorf("services = %+v", services)
	}

	if _, err := parseDNS(p[:len(p)-20]); err == nil {
		t.Error("truncated packet: expected an error")
	}
	if _, err := parseDNS(p[:4]); err == nil {
		t.Error("short packet: expected an error")
	}
}

// The frontend's sample data is also checked against web/src/types.ts, which keeps Go and TypeScript aligned.
func TestSampleMatchesMeshMap(t *testing.T) {
	data, err := os.ReadFile("web/dev/topology.json")
	if err != nil {
		t.Fatal(err)
	}
	var m meshMap
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sample doesn't round-trip through meshMap:\ngot  %s\nwant %s", encoded, data)
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
	h := routes(site, func(context.Context) (meshMap, error) { return meshMap{}, errors.New(reachMsg) })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Thread map") {
		t.Errorf("index: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/topology", nil))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Border Router") {
		t.Errorf("topology: %d %s", rec.Code, rec.Body)
	}
}
