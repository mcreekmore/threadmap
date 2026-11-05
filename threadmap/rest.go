package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	apiType      = "application/vnd.api+json"
	requestLimit = 15 * time.Second
	taskLimit    = 60 * time.Second
	discoverTask = `{"maxAge":30,"maxRetries":3,"deviceCount":1,"timeout":10}`
	diagTask     = `{"destination":%q,"types":["extAddress","rloc16","route","leaderData",%s],"timeout":10}`
	reachMsg     = "can't reach the border router's REST API. Is the OpenThread Border Router add-on started?"
)

var taskPoll = 500 * time.Millisecond

// borderRouter talks to the REST API of the OpenThread Border Router add-on.
type borderRouter struct {
	base string
}

// call never touches /node/dataset: its output contains the network key.
func (b borderRouter) call(ctx context.Context, method, path, body string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestLimit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", apiType)
	if body != "" {
		req.Header.Set("Content-Type", apiType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New(reachMsg)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, bytes.Join(bytes.Fields(data), nil))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

type task struct {
	ID         string `json:"id"`
	Attributes struct {
		Status string `json:"status"`
	} `json:"attributes"`
}

// runTask posts one task and returns its final status. Tasks run one at a time: of two queued together, one stopped.
func (b borderRouter) runTask(ctx context.Context, kind, attributes string) (string, error) {
	var posted struct{ Data []task }
	body := fmt.Sprintf(`{"data":[{"type":%q,"attributes":%s}]}`, kind, attributes)
	if err := b.call(ctx, http.MethodPost, "/api/actions", body, &posted); err != nil {
		return "", err
	}
	if len(posted.Data) != 1 {
		return "", fmt.Errorf("%s: no task returned", kind)
	}
	ctx, cancel := context.WithTimeout(ctx, taskLimit)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("%s: %w", kind, ctx.Err())
		case <-time.After(taskPoll):
		}
		var list struct{ Data []task }
		if err := b.call(ctx, http.MethodGet, "/api/actions", "", &list); err != nil {
			return "", err
		}
		for _, t := range list.Data {
			if s := t.Attributes.Status; t.ID == posted.Data[0].ID && s != "pending" && s != "active" {
				return s, nil
			}
		}
	}
}

type device struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Role string     `json:"role"`
		OMR  netip.Addr `json:"omrIpv6Address"`
	} `json:"attributes"`
}

type route struct {
	RouteID int `json:"routeId"`
	Out     int `json:"linkQualityOut"`
	In      int `json:"linkQualityIn"`
}

type diagnostic struct {
	Attributes struct {
		ExtAddress string `json:"extAddress"`
		Rloc16     string `json:"rloc16"`
		LeaderData *struct {
			LeaderRouterID int `json:"leaderRouterId"`
		} `json:"leaderData"`
		Route struct {
			RouteData []route `json:"routeData"`
		} `json:"route"`
		ChildTable []json.RawMessage `json:"childTable"`
		Children   []struct {
			Rloc16     string `json:"rloc16"`
			ExtAddress string `json:"extAddress"`
			LinkMargin int    `json:"linkMargin"`
		} `json:"children"`
		ChildIPv6Addresses []struct {
			Rloc16        string       `json:"rloc16"`
			IPv6Addresses []netip.Addr `json:"ipv6Addresses"`
		} `json:"childIpv6Addresses"`
	} `json:"attributes"`
}

func parseRLOC(s string) (uint16, error) {
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 16)
	return uint16(v), err
}

// marginQuality converts a link margin in dB to Thread's link quality scale.
func marginQuality(margin int) int {
	switch {
	case margin > 20:
		return 3
	case margin > 10:
		return 2
	case margin > 2:
		return 1
	}
	return 0
}

func toRouter(d diagnostic) (router, error) {
	a := d.Attributes
	rloc, err := parseRLOC(a.Rloc16)
	if err != nil {
		return router{}, fmt.Errorf("rloc16 %q: %w", a.Rloc16, err)
	}
	r := router{id: int(rloc >> 10), rloc: rloc, mac: a.ExtAddress, linksTo: map[int]int{}}
	for _, route := range a.Route.RouteData {
		// Route data lists every router, including itself; both qualities at 0 means no direct link.
		if route.RouteID != r.id && (route.In > 0 || route.Out > 0) {
			r.linksTo[route.RouteID] = min(route.In, route.Out)
		}
	}
	for _, c := range a.Children {
		if childRLOC, err := parseRLOC(c.Rloc16); err == nil {
			r.children = append(r.children, child{childRLOC, c.ExtAddress, marginQuality(c.LinkMargin)})
		}
	}
	return r, nil
}

// diagnose asks one router for its links plus childFields, a quoted JSON list such as `"childTable"`.
func (b borderRouter) diagnose(ctx context.Context, id, childFields string) (string, error) {
	return b.runTask(ctx, "getNetworkDiagnosticTask", fmt.Sprintf(diagTask, id, childFields))
}

// survey is one pass over the routers in OTBR's device collection.
type survey struct {
	routers  []router
	omrs     map[string]netip.Addr
	warnings []string
	missing  []string // What the device collection lacked; discovery may fill it in.
}

// scan reads topology over REST and names from discover, which runs alongside to keep the scan short.
// discover is sent the addresses it should name, so it can stop once it has them all.
func (b borderRouter) scan(ctx context.Context, discover func(context.Context, <-chan []netip.Addr) []service) (meshMap, error) {
	start := time.Now()
	want := make(chan []netip.Addr, 1)
	found := make(chan []service, 1)
	go func() { found <- discover(ctx, want) }()

	s, err := b.survey(ctx)
	if err != nil {
		return meshMap{}, err
	}
	log.Printf("timing: survey done at %v", time.Since(start))
	var warnings []string
	// Discovery never finishes before OTBR's 10s timeout on some meshes, so only run it when the collection falls short.
	if len(s.missing) > 0 {
		status, err := b.runTask(ctx, "updateDeviceCollectionTask", discoverTask)
		if err != nil {
			return meshMap{}, err
		}
		if status != "completed" {
			warnings = append(warnings, fmt.Sprintf("Device discovery ended as %q, so some devices may be missing.", status))
		}
		if s, err = b.survey(ctx); err != nil {
			return meshMap{}, err
		}
		log.Printf("timing: discovery and second survey done at %v", time.Since(start))
	}
	if len(s.routers) == 0 {
		return meshMap{}, errors.New("no router reported its links")
	}

	var addrs []netip.Addr
	for _, r := range s.routers {
		if !r.border {
			addrs = append(addrs, s.omrs[r.mac])
		}
		for _, c := range r.children {
			addrs = append(addrs, s.omrs[c.mac])
		}
	}
	want <- addrs
	services := <-found
	log.Printf("timing: scan done at %v", time.Since(start))
	m := buildMap(s.routers, s.omrs, services)
	m.Warnings = append(m.Warnings, warnings...)
	m.Warnings = append(m.Warnings, s.warnings...)
	m.Warnings = append(m.Warnings, s.missing...)
	return m, nil
}

// survey diagnoses every router in OTBR's device collection, which persists between scans.
func (b borderRouter) survey(ctx context.Context) (survey, error) {
	s := survey{omrs: map[string]netip.Addr{}}
	warnf := func(format string, args ...any) { s.warnings = append(s.warnings, fmt.Sprintf(format, args...)) }

	// Results from earlier scans would otherwise be read back as current.
	if err := b.call(ctx, http.MethodDelete, "/api/diagnostics", "", nil); err != nil {
		return s, err
	}
	var devices struct{ Data []device }
	if err := b.call(ctx, http.MethodGet, "/api/devices", "", &devices); err != nil {
		return s, err
	}
	border := ""
	for _, d := range devices.Data {
		s.omrs[d.ID] = d.Attributes.OMR
		if d.Type == "threadBorderRouter" {
			border = d.ID
		}
		if d.Attributes.Role == "child" {
			continue
		}
		status, err := b.diagnose(ctx, d.ID, `"childTable"`)
		if err != nil {
			return s, err
		}
		if status != "completed" {
			warnf("Router %s didn't report its links (%s).", d.ID, status)
		}
	}

	var diagnostics struct{ Data []diagnostic }
	if err := b.call(ctx, http.MethodGet, "/api/diagnostics", "", &diagnostics); err != nil {
		return s, err
	}
	// children has MACs and childIpv6Addresses their addresses, which the device collection often lacks.
	// They cost a full timeout on routers that don't answer them, so only ask when needed.
	asked := false
	for _, d := range diagnostics.Data {
		if len(d.Attributes.ChildTable) > 0 {
			asked = true
			if status, err := b.diagnose(ctx, d.Attributes.ExtAddress, `"children","childIpv6Addresses"`); err != nil {
				return s, err
			} else if status != "completed" {
				warnf("Router %s didn't report its children (%s).", d.Attributes.ExtAddress, status)
			}
		}
	}
	if asked {
		if err := b.call(ctx, http.MethodGet, "/api/diagnostics", "", &diagnostics); err != nil {
			return s, err
		}
		// Each router now has a second entry with its children; keep only that one.
		latest := map[string]int{}
		var kept []diagnostic
		for _, d := range diagnostics.Data {
			i, seen := latest[d.Attributes.ExtAddress]
			switch {
			case !seen:
				latest[d.Attributes.ExtAddress] = len(kept)
				kept = append(kept, d)
			case len(d.Attributes.Children) > 0:
				kept[i] = d
			}
		}
		diagnostics.Data = kept
	}

	// Children's OMR addresses share the /64 of the routers' ones.
	omrPrefixes := map[netip.Prefix]bool{}
	for _, addr := range s.omrs {
		if p, err := addr.Prefix(64); err == nil {
			omrPrefixes[p] = true
		}
	}
	for _, d := range diagnostics.Data {
		for _, c := range d.Attributes.ChildIPv6Addresses {
			for _, child := range d.Attributes.Children {
				if child.Rloc16 != c.Rloc16 || s.omrs[child.ExtAddress].IsValid() {
					continue
				}
				for _, addr := range c.IPv6Addresses {
					if p, err := addr.Prefix(64); err == nil && omrPrefixes[p] {
						s.omrs[child.ExtAddress] = addr
					}
				}
			}
		}
	}

	leaderID := -1
	for _, d := range diagnostics.Data {
		if d.Attributes.LeaderData != nil {
			leaderID = d.Attributes.LeaderData.LeaderRouterID
		}
	}
	reported := map[int]bool{}
	for _, d := range diagnostics.Data {
		r, err := toRouter(d)
		if err != nil {
			warnf("Skipped router %s: %v", d.Attributes.ExtAddress, err)
			continue
		}
		r.border = r.mac == border
		r.leader = r.id == leaderID
		s.routers = append(s.routers, r)
		reported[r.id] = true
	}

	// Every router lists every router ID in use, so any ID that didn't report is one the collection lacks.
	missed := map[int]bool{}
	for _, d := range diagnostics.Data {
		for _, route := range d.Attributes.Route.RouteData {
			if !reported[route.RouteID] && !missed[route.RouteID] {
				missed[route.RouteID] = true
				s.missing = append(s.missing, fmt.Sprintf("Router %s didn't report its links.", rlocString(uint16(route.RouteID<<10))))
			}
		}
	}
	for _, r := range s.routers {
		for _, c := range r.children {
			if !s.omrs[c.mac].IsValid() {
				s.missing = append(s.missing, fmt.Sprintf("Child %s has no known address, so it can't be named.", c.mac))
			}
		}
	}
	if len(s.routers) == 0 {
		s.missing = append(s.missing, "No router reported its links.")
	}
	return s, nil
}
