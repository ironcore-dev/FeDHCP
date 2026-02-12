// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package management

import (
	"fmt"
	"net"
	"slices"
	"testing"
)

func TestFeEUI64(t *testing.T) {
	tests := []struct {
		ip   net.IP
		mac  net.HardwareAddr
		want net.IP
	}{{
		net.ParseIP("2001:db8::"),
		parseMAC("aa:bb:cc:dd:ee:ff"),
		net.ParseIP("2001:db8::aabb:ccfe:fedd:eeff"),
	}, {
		net.ParseIP("2001:db8::"),
		parseMAC("01:23:45:67:89:ab"),
		net.ParseIP("2001:db8::0123:45fe:fe67:89ab"),
	}, {
		net.ParseIP("2001:db8::dead:beef"),
		parseMAC("aa:bb:cc:dd:ee:ff"),
		net.ParseIP("2001:db8::aabb:ccfe:fedd:eeff"),
	}}

	for ti, tt := range tests {
		t.Run(fmt.Sprintf("#%d", ti), func(t *testing.T) {
			feEUI64(tt.ip, tt.mac)
			if !slices.Equal(tt.ip, tt.want) {
				t.Errorf("got=%s != want=%s", tt.ip.String(), tt.want.String())
			}
		})
	}
}

func parseMAC(s string) net.HardwareAddr {
	a, err := net.ParseMAC(s)
	if err != nil {
		panic(err.Error())
	}

	return a
}
