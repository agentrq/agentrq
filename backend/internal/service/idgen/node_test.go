// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package idgen

import (
	"errors"
	"net"
	"testing"

	"github.com/cespare/xxhash/v2"
)

func iface(index int, flags net.Flags, mac string) net.Interface {
	hw, _ := net.ParseMAC(mac)
	return net.Interface{Index: index, Flags: flags, HardwareAddr: hw}
}

func listing(ifaces ...net.Interface) func() ([]net.Interface, error) {
	return func() ([]net.Interface, error) { return ifaces, nil }
}

func nodeOf(mac string) uint16 {
	hw, _ := net.ParseMAC(mac)
	return uint16(xxhash.Sum64(hw) % MaxNodes)
}

func TestNodeFromMAC(t *testing.T) {
	const eth0, docker0 = "52:54:00:12:34:56", "02:42:ac:11:00:01"
	up := net.FlagUp

	tests := []struct {
		name   string
		ifaces []net.Interface
		want   uint16
	}{
		{
			name:   "the only usable interface",
			ifaces: []net.Interface{iface(2, up, eth0)},
			want:   nodeOf(eth0),
		},
		{
			name:   "the lowest index wins, whatever the listing order",
			ifaces: []net.Interface{iface(5, up, docker0), iface(2, up, eth0)},
			want:   nodeOf(eth0),
		},
		{
			name: "loopback, down and MAC-less interfaces are skipped",
			ifaces: []net.Interface{
				iface(1, up|net.FlagLoopback, "00:00:00:00:00:01"),
				iface(2, 0, eth0),
				{Index: 3, Flags: up},
				iface(4, up, "00:00:00:00:00:00"),
				iface(7, up, docker0),
			},
			want: nodeOf(docker0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NodeFromMAC(listing(tt.ifaces...))
			if err != nil {
				t.Fatalf("NodeFromMAC: %v", err)
			}
			if got != tt.want {
				t.Errorf("node = %d, want %d", got, tt.want)
			}
			if got >= MaxNodes {
				t.Errorf("node %d is out of monoflake's range", got)
			}
		})
	}
}

func TestNodeFromMACIsStable(t *testing.T) {
	ifaces := listing(iface(2, net.FlagUp, "52:54:00:12:34:56"))
	first, _ := NodeFromMAC(ifaces)
	again, _ := NodeFromMAC(ifaces)
	if first != again {
		t.Errorf("node changed between calls: %d then %d", first, again)
	}
}

func TestNodeFromMACWithoutAnInterface(t *testing.T) {
	_, err := NodeFromMAC(listing(iface(1, net.FlagUp|net.FlagLoopback, "00:00:00:00:00:01")))
	if !errors.Is(err, ErrNoMAC) {
		t.Errorf("err = %v, want ErrNoMAC", err)
	}
}

func TestNodeFromMACListingFails(t *testing.T) {
	denied := errors.New("netlink: permission denied")
	_, err := NodeFromMAC(func() ([]net.Interface, error) { return nil, denied })
	if !errors.Is(err, denied) {
		t.Errorf("err = %v, want it to wrap %v", err, denied)
	}
}
