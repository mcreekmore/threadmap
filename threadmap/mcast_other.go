//go:build !linux

package main

import "net"

// The add-on only runs on Linux; this lets the tests build elsewhere.
func setMulticastInterface(*net.UDPConn, net.IP) error { return nil }
