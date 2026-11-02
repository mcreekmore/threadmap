package main

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type router struct {
	id      int
	rloc    uint16
	mac     string
	leader  bool
	border  bool
	linksTo map[int]int // router ID -> link quality
}

type service struct {
	name  string
	addrs []netip.Addr
}

var (
	routerLine = regexp.MustCompile(`^id:(\d+) rloc16:0x([0-9a-fA-F]+) ext-addr:([0-9a-fA-F]+)(.*)$`)
	linkLine   = regexp.MustCompile(`^\s*(\d+)-links:\{([^}]*)\}`)
)

// The ot-ctl output format isn't a stable API, so every parser skips lines it doesn't recognize.

func parseTopology(out string) []router {
	var routers []router
	for _, line := range strings.Split(out, "\n") {
		if m := routerLine.FindStringSubmatch(line); m != nil {
			id, err := strconv.Atoi(m[1])
			rloc, err2 := strconv.ParseUint(m[2], 16, 16)
			if err != nil || err2 != nil {
				continue
			}
			flags := strings.Fields(m[4])
			routers = append(routers, router{
				id:      id,
				rloc:    uint16(rloc),
				mac:     m[3],
				leader:  contains(flags, "leader"),
				border:  contains(flags, "br"),
				linksTo: map[int]int{},
			})
		} else if m := linkLine.FindStringSubmatch(line); m != nil && len(routers) > 0 {
			quality, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			for _, field := range strings.Fields(m[2]) {
				if id, err := strconv.Atoi(field); err == nil {
					routers[len(routers)-1].linksTo[id] = quality
				}
			}
		}
	}
	return routers
}

func parseServices(out string) []service {
	var services []service
	var cur *service
	flush := func() {
		if cur != nil {
			services = append(services, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case line != "" && line[0] != ' ' && line[0] != '\t':
			flush()
			if name, _, ok := strings.Cut(line, "._"); ok {
				cur = &service{name: name}
			}
		case cur == nil:
		case trimmed == "deleted: true":
			cur = nil
		case strings.HasPrefix(trimmed, "addresses:"):
			list := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "addresses:")), "[]")
			for _, field := range strings.Split(list, ",") {
				if addr, err := netip.ParseAddr(strings.TrimSpace(field)); err == nil {
					cur.addrs = append(cur.addrs, addr)
				}
			}
		}
	}
	flush()
	return services
}

func parseEIDCache(out string) map[netip.Addr]uint16 {
	cache := map[netip.Addr]uint16{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		addr, err := netip.ParseAddr(fields[0])
		rloc, err2 := strconv.ParseUint(strings.TrimPrefix(fields[1], "0x"), 16, 16)
		// 0xfffe marks a lookup still in progress, so there is no location yet.
		if err != nil || err2 != nil || rloc == 0xfffe {
			continue
		}
		cache[addr] = uint16(rloc)
	}
	return cache
}

// locate returns the RLOC16 of a service, ignoring cache entries that point at a router no longer in the mesh.
func locate(s service, routerIDs map[int]bool, cache map[netip.Addr]uint16) (uint16, bool) {
	for _, addr := range s.addrs {
		if rloc, ok := cache[addr]; ok && routerIDs[int(rloc>>10)] {
			return rloc, true
		}
	}
	return 0, false
}

func routerIDs(routers []router) map[int]bool {
	ids := map[int]bool{}
	for _, r := range routers {
		ids[r.id] = true
	}
	return ids
}

// pingTargets returns one address for each named device the cache can't place yet.
func pingTargets(routers []router, services []service, cache map[netip.Addr]uint16) []netip.Addr {
	ids := routerIDs(routers)
	var targets []netip.Addr
	for _, s := range services {
		if _, ok := locate(s, ids, cache); !ok && len(s.addrs) > 0 {
			targets = append(targets, s.addrs[0])
		}
	}
	return targets
}

type node struct {
	RLOC   string `json:"rloc"`
	Label  string `json:"label"`
	MAC    string `json:"mac,omitempty"`
	Kind   string `json:"kind"` // router or child
	Leader bool   `json:"leader,omitempty"`
	Border bool   `json:"border,omitempty"`
	names  []string
	key    uint16
}

type link struct {
	A       string `json:"a"`
	B       string `json:"b"`
	Quality *int   `json:"quality"` // nil for a child's link, whose quality isn't reported
}

type meshMap struct {
	Nodes    []*node  `json:"nodes"`
	Links    []link   `json:"links"`
	Warnings []string `json:"warnings"`
}

func rlocString(rloc uint16) string { return fmt.Sprintf("0x%04x", rloc) }

func buildMap(routers []router, services []service, cache map[netip.Addr]uint16) meshMap {
	m := meshMap{Nodes: []*node{}, Links: []link{}, Warnings: []string{}}
	byRLOC := map[uint16]*node{}
	rlocByID := map[int]uint16{}
	for _, r := range routers {
		n := &node{key: r.rloc, RLOC: rlocString(r.rloc), MAC: r.mac, Kind: "router", Leader: r.leader, Border: r.border}
		byRLOC[r.rloc] = n
		rlocByID[r.id] = r.rloc
		m.Nodes = append(m.Nodes, n)
	}

	ids := routerIDs(routers)
	for _, s := range services {
		rloc, ok := locate(s, ids, cache)
		if !ok {
			m.Warnings = append(m.Warnings, fmt.Sprintf("%s is registered but wasn't located in the mesh; it may be asleep or offline.", s.name))
			continue
		}
		n := byRLOC[rloc]
		if n == nil { // Non-zero low bits: a child of router rloc>>10.
			n = &node{key: rloc, RLOC: rlocString(rloc), Kind: "child"}
			byRLOC[rloc] = n
			m.Nodes = append(m.Nodes, n)
			m.Links = append(m.Links, link{A: rlocString(rlocByID[int(rloc>>10)]), B: n.RLOC})
		}
		if !contains(n.names, s.name) {
			n.names = append(n.names, s.name)
		}
	}

	for _, n := range m.Nodes {
		switch {
		case len(n.names) > 0:
			n.Label = strings.Join(n.names, ", ")
		case n.Border:
			n.Label = "Border router"
		default:
			n.Label = n.MAC
		}
	}

	// Each router reports the link separately, so keep one edge at the lower reported quality.
	lowest := map[[2]int]int{}
	for _, r := range routers {
		for to, quality := range r.linksTo {
			if !ids[to] {
				continue
			}
			pair := [2]int{min(r.id, to), max(r.id, to)}
			if q, seen := lowest[pair]; !seen || quality < q {
				lowest[pair] = quality
			}
		}
	}
	for pair, quality := range lowest {
		q := quality
		m.Links = append(m.Links, link{A: rlocString(rlocByID[pair[0]]), B: rlocString(rlocByID[pair[1]]), Quality: &q})
	}

	sort.Slice(m.Nodes, func(i, j int) bool { return m.Nodes[i].key < m.Nodes[j].key })
	sort.Slice(m.Links, func(i, j int) bool {
		if m.Links[i].A != m.Links[j].A {
			return m.Links[i].A < m.Links[j].A
		}
		return m.Links[i].B < m.Links[j].B
	})
	return m
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
