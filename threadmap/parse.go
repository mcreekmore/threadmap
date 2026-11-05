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
	linksTo  map[int]route // router ID -> link qualities in both directions
	children []child
}

type child struct {
	rloc   uint16
	mac    string
	margin int // dB, as the parent hears the child
}

type service struct {
	name   string
	addrs  []netip.Addr
	matter bool      // name is a Matter "<fabric ID>-<node ID>", not a friendly name
	hapID  string    // HomeKit pairing ID, from the TXT record
	ha     *haDevice // set once matched to an HA device
}

type node struct {
	RLOC   string     `json:"rloc"`
	Label  string     `json:"label"`
	MAC    string     `json:"mac,omitempty"`
	Kind   string     `json:"kind"` // router or child
	Leader bool       `json:"leader,omitempty"`
	Border bool       `json:"border,omitempty"`
	OMR    netip.Addr `json:"omr,omitzero"`
	HA     *haDevice  `json:"ha,omitempty"`
	names  []string
}

type link struct {
	A       string `json:"a"`
	B       string `json:"b"`
	Quality int    `json:"quality"` // the weaker direction
	// Router links know both directions. Child links know only how the parent (A) hears the child (B).
	AToB   *int `json:"aToB,omitempty"`
	BToA   *int `json:"bToA,omitempty"`
	Margin *int `json:"margin,omitempty"` // dB
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
		m.Nodes = append(m.Nodes, &node{RLOC: rlocString(r.rloc), MAC: r.mac, Kind: "router", Leader: r.leader, Border: r.border})
		rlocByID[r.id] = r.rloc
	}
	for _, r := range routers {
		for _, c := range r.children {
			n := &node{RLOC: rlocString(c.rloc), MAC: c.mac, Kind: "child"}
			m.Nodes = append(m.Nodes, n)
			quality := marginQuality(c.margin)
			m.Links = append(m.Links, link{A: rlocString(r.rloc), B: n.RLOC, Quality: quality, BToA: &quality, Margin: &c.margin})
		}
	}

	byAddr := map[netip.Addr]*node{}
	for _, n := range m.Nodes {
		n.OMR = omrs[n.MAC]
		byAddr[n.OMR] = n
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
				if n.HA == nil {
					n.HA = s.ha
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

	// Both ends of a link report it, so keep the lower reported quality for each direction.
	lowest := map[[2]int]int{} // [from, to] router IDs -> quality
	keep := func(from, to, quality int) {
		if q, seen := lowest[[2]int{from, to}]; !seen || quality < q {
			lowest[[2]int{from, to}] = quality
		}
	}
	for _, r := range routers {
		for to, route := range r.linksTo {
			if _, known := rlocByID[to]; known {
				keep(r.id, to, route.Out)
				keep(to, r.id, route.In)
			}
		}
	}
	for pair, aToB := range lowest {
		if pair[0] < pair[1] {
			bToA := lowest[[2]int{pair[1], pair[0]}]
			m.Links = append(m.Links, link{A: rlocString(rlocByID[pair[0]]), B: rlocString(rlocByID[pair[1]]), Quality: min(aToB, bToA), AToB: &aToB, BToA: &bToA})
		}
	}

	sort.Slice(m.Nodes, func(i, j int) bool { return m.Nodes[i].RLOC < m.Nodes[j].RLOC })
	sort.Slice(m.Links, func(i, j int) bool {
		if m.Links[i].A != m.Links[j].A {
			return m.Links[i].A < m.Links[j].A
		}
		return m.Links[i].B < m.Links[j].B
	})
	return m
}
