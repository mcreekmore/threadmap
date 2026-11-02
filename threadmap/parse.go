package main

import (
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"
)

type router struct {
	id       int
	rloc     uint16
	mac      string
	leader   bool
	border   bool
	linksTo  map[int]int // router ID -> link quality
	children []child
}

type child struct {
	rloc    uint16
	mac     string
	quality int
}

type service struct {
	name  string
	addrs []netip.Addr
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
	Quality int    `json:"quality"`
}

type meshMap struct {
	Nodes    []*node  `json:"nodes"`
	Links    []link   `json:"links"`
	Warnings []string `json:"warnings"`
}

func rlocString(rloc uint16) string { return fmt.Sprintf("0x%04x", rloc) }

// buildMap names devices by matching each service's addresses to the OMR address reported per MAC.
func buildMap(routers []router, omrs map[string]netip.Addr, services []service) meshMap {
	m := meshMap{Nodes: []*node{}, Links: []link{}, Warnings: []string{}}
	rlocByID := map[int]uint16{}
	for _, r := range routers {
		m.Nodes = append(m.Nodes, &node{key: r.rloc, RLOC: rlocString(r.rloc), MAC: r.mac, Kind: "router", Leader: r.leader, Border: r.border})
		rlocByID[r.id] = r.rloc
	}
	for _, r := range routers {
		for _, c := range r.children {
			n := &node{key: c.rloc, RLOC: rlocString(c.rloc), MAC: c.mac, Kind: "child"}
			m.Nodes = append(m.Nodes, n)
			m.Links = append(m.Links, link{A: rlocString(r.rloc), B: n.RLOC, Quality: c.quality})
		}
	}

	byAddr := map[netip.Addr]*node{}
	for _, n := range m.Nodes {
		byAddr[omrs[n.MAC]] = n
	}
	prefixes := map[netip.Prefix]bool{}
	for _, addr := range omrs {
		if p, err := addr.Prefix(64); err == nil {
			prefixes[p] = true
		}
	}
	for _, s := range services {
		located, onMesh := false, false
		for _, addr := range s.addrs {
			if p, err := addr.Prefix(64); err == nil && prefixes[p] {
				onMesh = true
			}
			if n := byAddr[addr]; n != nil {
				located = true
				if !slices.Contains(n.names, s.name) {
					n.names = append(n.names, s.name)
				}
			}
		}
		// Off-mesh addresses are Wi-Fi or Ethernet devices using the same service types, like Matter over LAN.
		if onMesh && !located {
			m.Warnings = append(m.Warnings, fmt.Sprintf("%s is registered but wasn't located in the mesh; it may be asleep or offline.", s.name))
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
			if _, known := rlocByID[to]; !known {
				continue
			}
			pair := [2]int{min(r.id, to), max(r.id, to)}
			if q, seen := lowest[pair]; !seen || quality < q {
				lowest[pair] = quality
			}
		}
	}
	for pair, quality := range lowest {
		m.Links = append(m.Links, link{A: rlocString(rlocByID[pair[0]]), B: rlocString(rlocByID[pair[1]]), Quality: quality})
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
