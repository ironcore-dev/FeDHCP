// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package stateless

import (
	"fmt"
	"net"
	"os"
	"slices"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"gopkg.in/yaml.v3"

	"github.com/ironcore-dev/fedhcp/internal/api"
)

var expectedIAID = [4]byte{1, 2, 3, 4}

func initPlugin(prefixLen int) {
	config := api.StatelessConfig{PrefixLength: prefixLen}
	configData, _ := yaml.Marshal(config)

	file, _ := os.CreateTemp("", "stateless_config_*.yaml")
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	_ = os.WriteFile(file.Name(), configData, 0644)

	_, err := setup6(file.Name())
	if err != nil {
		log.Fatal(err)
	}
}

func TestSetup6Validation(t *testing.T) {
	_, err := setup6()
	if err == nil {
		t.Fatal("expected error with no arguments")
	}

	_, err = setup6("non-existing.yaml")
	if err == nil {
		t.Fatal("expected error with non-existing file")
	}

	_, err = setup6("foo", "bar")
	if err == nil {
		t.Fatal("expected error with two arguments")
	}

	writeConfig := func(pl int) string {
		config := api.StatelessConfig{PrefixLength: pl}
		data, _ := yaml.Marshal(config)
		f, _ := os.CreateTemp("", "stateless_config_*.yaml")
		_ = os.WriteFile(f.Name(), data, 0644)
		_ = f.Close()
		return f.Name()
	}

	path := writeConfig(96)
	defer os.Remove(path)
	_, err = setup6(path)
	if err == nil {
		t.Fatal("expected error for prefixLength 96")
	}

	path = writeConfig(0)
	defer os.Remove(path)
	_, err = setup6(path)
	if err == nil {
		t.Fatal("expected error for prefixLength 0")
	}

	path = writeConfig(64)
	defer os.Remove(path)
	_, err = setup6(path)
	if err != nil {
		t.Fatalf("unexpected error for prefixLength 64: %v", err)
	}

	path = writeConfig(80)
	defer os.Remove(path)
	_, err = setup6(path)
	if err != nil {
		t.Fatalf("unexpected error for prefixLength 80: %v", err)
	}
}

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
			got := feEUI64(tt.ip, tt.mac)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got=%s != want=%s", got.String(), tt.want.String())
			}
		})
	}
}

func TestBuildAddressFromMAC(t *testing.T) {
	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")
	mac := parseMAC("aa:bb:cc:dd:ee:ff")
	expected := net.IP{0x20, 0x01, 0x0d, 0xb8, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	got := buildAddressFromMAC(linkAddr, mac)
	if !slices.Equal(got, expected) {
		t.Errorf("got=%s != want=%s", got.String(), expected)
	}
}

func TestHandler6_PrefixLength64(t *testing.T) {
	initPlugin(64)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{IaId: expectedIAID})

	// MAC aa:bb:cc:dd:ee:ff -> EUI-64 modified: a8:bb:cc:ff:fe:dd:ee:ff
	peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
	linkAddr := net.ParseIP("2001:db8::")

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, peerAddr)
	if err != nil {
		t.Fatal(err)
	}

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(relayedRequest, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	iana := resp.(*dhcpv6.Message).Options.OneIANA()
	if iana == nil {
		t.Fatal("expected IANA option in response")
	}

	addr := iana.Options.OneAddress().IPv6Addr
	expectedAddr := feEUI64(linkAddr, parseMAC("aa:bb:cc:dd:ee:ff"))
	if !addr.Equal(expectedAddr) {
		t.Errorf("expected address %s, got %s", expectedAddr, addr)
	}

	if iana.IaId != expectedIAID {
		t.Errorf("expected IAID %v, got %v", expectedIAID, iana.IaId)
	}

	preferred := iana.Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime
	valid := iana.Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime
	if preferred != preferredLifeTime {
		t.Errorf("expected preferred lifetime %v, got %v", preferredLifeTime, preferred)
	}
	if valid != validLifeTime {
		t.Errorf("expected valid lifetime %v, got %v", validLifeTime, valid)
	}
}

func TestHandler6_PrefixLength80(t *testing.T) {
	initPlugin(80)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{IaId: expectedIAID})

	peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, peerAddr)
	if err != nil {
		t.Fatal(err)
	}

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(relayedRequest, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	iana := resp.(*dhcpv6.Message).Options.OneIANA()
	if iana == nil {
		t.Fatal("expected IANA option in response")
	}

	addr := iana.Options.OneAddress().IPv6Addr
	expectedAddr := net.IP{0x20, 0x01, 0x0d, 0xb8, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	if !addr.Equal(expectedAddr) {
		t.Errorf("expected address %s, got %s", expectedAddr, addr)
	}

	if iana.IaId != expectedIAID {
		t.Errorf("expected IAID %v, got %v", expectedIAID, iana.IaId)
	}

	preferred := iana.Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime
	valid := iana.Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime
	if preferred != preferredLifeTime {
		t.Errorf("expected preferred lifetime %v, got %v", preferredLifeTime, preferred)
	}
	if valid != validLifeTime {
		t.Errorf("expected valid lifetime %v, got %v", validLifeTime, valid)
	}
}

func TestHandler6_NoIANA(t *testing.T) {
	initPlugin(80)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest

	peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, peerAddr)
	if err != nil {
		t.Fatal(err)
	}

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(relayedRequest, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}
	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	opts := resp.GetOption(dhcpv6.OptionIANA)
	if len(opts) != 0 {
		t.Fatalf("expected 0 IANA options, got %d: %v", len(opts), opts)
	}
}

func TestHandler6_NoRelay(t *testing.T) {
	initPlugin(64)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{IaId: expectedIAID})

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(req, stub)
	if resp != nil {
		t.Fatal("plugin should not return a message for non-relay")
	}
	if !stop {
		t.Error("plugin did not interrupt processing, but it should have")
	}
}

func TestHandler6_LinkAddrNotAligned(t *testing.T) {
	tests := []struct {
		name         string
		prefixLength int
		linkAddr     net.IP
	}{{
		name:         "/64 misaligned",
		prefixLength: 64,
		linkAddr:     net.ParseIP("2001:db8::1"),
	}, {
		name:         "/80 misaligned",
		prefixLength: 80,
		linkAddr:     net.ParseIP("2001:db8::1"),
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initPlugin(tt.prefixLength)

			req, err := dhcpv6.NewMessage()
			if err != nil {
				t.Fatal(err)
			}
			req.MessageType = dhcpv6.MessageTypeRequest
			req.AddOption(&dhcpv6.OptIANA{IaId: expectedIAID})

			peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
			relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, tt.linkAddr, peerAddr)
			if err != nil {
				t.Fatal(err)
			}

			stub, err := dhcpv6.NewMessage()
			if err != nil {
				t.Fatal(err)
			}
			stub.MessageType = dhcpv6.MessageTypeReply

			resp, stop := handler6(relayedRequest, stub)
			if resp != nil {
				t.Fatal("plugin should not return a message for misaligned LinkAddr")
			}
			if !stop {
				t.Error("plugin did not interrupt processing, but it should have")
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
