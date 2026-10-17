// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package idgen

import (
	"errors"
	"fmt"
	"net"

	"github.com/cespare/xxhash/v2"
)

// MaxNodes is how many node ids monoflake tells apart: it keeps 10 bits.
const MaxNodes = 1024

// ErrNoMAC is [NodeFromMAC] finding no interface to derive a node id from.
var ErrNoMAC = errors.New("idgen: no network interface with a MAC address")

// NodeFromMAC derives a node id from this machine's MAC address, for an
// instance whose node id is not configured.
//
// The interface is the up, non-loopback one with the lowest index, not the
// first one listed: the bridges and veths a container host adds later get
// higher indexes, so they cannot move the node id between restarts.
func NodeFromMAC(interfaces func() ([]net.Interface, error)) (uint16, error) {
	ifaces, err := interfaces()
	if err != nil {
		return 0, fmt.Errorf("idgen: list network interfaces: %w", err)
	}
	var mac net.HardwareAddr
	lowest := 0
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || isZero(i.HardwareAddr) {
			continue
		}
		if mac == nil || i.Index < lowest {
			mac, lowest = i.HardwareAddr, i.Index
		}
	}
	if mac == nil {
		return 0, ErrNoMAC
	}
	return uint16(xxhash.Sum64(mac) % MaxNodes), nil
}

// isZero is an absent MAC, or the all-zero one tunnels report.
func isZero(mac net.HardwareAddr) bool {
	for _, b := range mac {
		if b != 0 {
			return false
		}
	}
	return true
}
