package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	typePTR  = 12
	typeAAAA = 28
	typeSRV  = 33
)

var serviceTypes = []string{"_hap._udp.local", "_matter._tcp.local"}

// browseLimit caps the browse for devices that never answer, like ones that registered no service.
const browseLimit = 6 * time.Second

type record struct {
	name  string
	kind  uint16
	value string
}

// browse asks the LAN which Thread services exist; the border router republishes them there from SRP.
// It stops once every address from want and every service found has been resolved, or after browseLimit.
func browse(ctx context.Context, want <-chan []netip.Addr) []service {
	var mu sync.Mutex
	var records []record

	socks := multicastSockets()
	for _, c := range socks {
		defer c.Close()
		go func() {
			buf := make([]byte, 9000)
			for {
				n, _, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				if rs, err := parseDNS(buf[:n]); err == nil {
					mu.Lock()
					records = append(records, rs...)
					mu.Unlock()
				}
			}
		}()
	}

	group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	send := func(name string, qtype uint16) {
		for _, c := range socks {
			c.WriteToUDP(dnsQuery(name, qtype), group)
		}
	}
	var wanted []netip.Addr
	known := false
	deadline := time.After(browseLimit)
	queried := map[string]bool{}
	for tick := 0; ; tick++ {
		// Replies repeat records; ask each name once, but everything again every 2s in case a reply was lost.
		if tick%8 == 0 {
			clear(queried)
			for _, t := range serviceTypes {
				send(t, typePTR)
			}
		}
		mu.Lock()
		seen := slices.Clone(records)
		mu.Unlock()
		for _, r := range seen {
			if queried[r.value] {
				continue
			}
			queried[r.value] = true
			switch r.kind {
			case typePTR:
				send(r.value, typeSRV)
			case typeSRV:
				send(r.value, typeAAAA)
			}
		}
		if known && resolved(seen, wanted) {
			return toServices(seen)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-deadline:
			return toServices(seen)
		case wanted = <-want:
			known, want = true, nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// resolved reports whether every wanted address and every instance and host seen so far has its records.
// ponytail: a responder that first answers after the others are resolved is missed; if that shows up, also wait a minimum time.
func resolved(records []record, want []netip.Addr) bool {
	have := map[string]bool{} // instances with an SRV, hosts with an AAAA
	addrs := map[netip.Addr]bool{}
	for _, r := range records {
		switch r.kind {
		case typeSRV:
			have[strings.ToLower(r.name)] = true
		case typeAAAA:
			have[strings.ToLower(r.name)] = true
			if a, err := netip.ParseAddr(r.value); err == nil {
				addrs[a] = true
			}
		}
	}
	for _, r := range records {
		if (r.kind == typePTR || r.kind == typeSRV) && !have[strings.ToLower(r.value)] {
			return false
		}
	}
	for _, a := range want {
		if !addrs[a] {
			return false
		}
	}
	return true
}

// multicastSockets opens one socket per IPv4 address. Their ephemeral source ports ask for unicast
// replies, so port 5353 can stay taken by other software on the host.
func multicastSockets() []*net.UDPConn {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("mDNS: %v", err)
	}
	var socks []*net.UDPConn
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: n.IP})
			if err == nil {
				err = setMulticastInterface(c, n.IP)
			}
			if err != nil {
				log.Printf("mDNS on %s: %v", ifi.Name, err)
				continue
			}
			socks = append(socks, c)
		}
	}
	return socks
}

// toServices joins each instance of serviceTypes to its host's addresses.
func toServices(records []record) []service {
	addrs := map[string][]netip.Addr{}
	for _, r := range records {
		if r.kind != typeAAAA {
			continue
		}
		host := strings.ToLower(r.name)
		if addr, err := netip.ParseAddr(r.value); err == nil && !slices.Contains(addrs[host], addr) {
			addrs[host] = append(addrs[host], addr)
		}
	}
	var services []service
	seen := map[string]bool{}
	for _, r := range records {
		for _, t := range serviceTypes {
			if name, ok := strings.CutSuffix(r.name, "."+t); ok && r.kind == typeSRV && !seen[r.name] {
				seen[r.name] = true
				services = append(services, service{name, addrs[strings.ToLower(r.value)], t == "_matter._tcp.local"})
			}
		}
	}
	return services
}

func dnsQuery(name string, qtype uint16) []byte {
	q := make([]byte, 12)
	binary.BigEndian.PutUint16(q[4:], 1)
	for _, label := range strings.Split(name, ".") {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0)
	q = binary.BigEndian.AppendUint16(q, qtype)
	return binary.BigEndian.AppendUint16(q, 1)
}

func readName(d []byte, i int) (string, int) {
	var labels []string
	end := -1
	for range 64 {
		n := int(d[i])
		switch {
		case n >= 0xC0:
			if end < 0 {
				end = i + 2
			}
			i = (n&0x3F)<<8 | int(d[i+1])
		case n == 0:
			if end < 0 {
				end = i + 1
			}
			return strings.Join(labels, "."), end
		default:
			labels = append(labels, string(d[i+1:i+1+n]))
			i += n + 1
		}
	}
	panic("name loop")
}

// parseDNS reads packets from the LAN, so a malformed one returns an error instead of panicking.
func parseDNS(d []byte) (out []record, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("bad packet: %v", r)
		}
	}()
	be := binary.BigEndian
	questions := int(be.Uint16(d[4:]))
	answers := int(be.Uint16(d[6:])) + int(be.Uint16(d[8:])) + int(be.Uint16(d[10:]))
	i := 12
	for range questions {
		_, i = readName(d, i)
		i += 4
	}
	for range answers {
		var name string
		name, i = readName(d, i)
		kind := be.Uint16(d[i:])
		length := int(be.Uint16(d[i+8:]))
		i += 10
		switch kind {
		case typePTR:
			v, _ := readName(d, i)
			out = append(out, record{name, kind, v})
		case typeSRV:
			v, _ := readName(d, i+6)
			out = append(out, record{name, kind, v})
		case typeAAAA:
			if addr, ok := netip.AddrFromSlice(d[i : i+length]); ok {
				out = append(out, record{name, kind, addr.String()})
			}
		}
		i += length
	}
	return out, nil
}
