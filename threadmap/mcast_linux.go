package main

import (
	"net"
	"syscall"
)

// Multicast loopback stays on (the Linux default): the border router answers from this same host.
func setMulticastInterface(c *net.UDPConn, ip net.IP) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var addr [4]byte
	copy(addr[:], ip.To4())
	var opErr error
	if err := raw.Control(func(fd uintptr) {
		opErr = syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, addr)
		if opErr == nil {
			opErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255)
		}
	}); err != nil {
		return err
	}
	return opErr
}
