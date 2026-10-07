// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package stateless

import (
	"net"
	"slices"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
)

var expectedIAID = [4]byte{1, 2, 3, 4}

func TestBuildAddressFromMAC(t *testing.T) {
	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")
	mac := parseMAC("aa:bb:cc:dd:ee:ff")
	expected := net.IP{0x20, 0x01, 0x0d, 0xb8, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	got := buildAddressFromMAC(linkAddr, mac)
	if !slices.Equal(got, expected) {
		t.Errorf("got=%s != want=%s", got.String(), expected)
	}
}

func TestHandler6_PrefixLength80(t *testing.T) {
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
	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{IaId: expectedIAID})

	peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
	linkAddr := net.ParseIP("2001:db8::1")

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
	if resp != nil {
		t.Fatal("plugin should not return a message for misaligned LinkAddr")
	}
	if !stop {
		t.Error("plugin did not interrupt processing, but it should have")
	}
}

func TestHandler6_ClientLinkLayerAddress(t *testing.T) {
	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{IaId: expectedIAID})

	// peer address of 11:22:33:44:55:66, but the relay reports aa:bb:cc:dd:ee:ff (RFC6939)
	peerAddr := net.ParseIP("fe80::1322:33ff:fe44:5566")
	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, peerAddr)
	if err != nil {
		t.Fatal(err)
	}
	relayedRequest.AddOption(dhcpv6.OptClientLinkLayerAddress(iana.HWTypeEthernet, parseMAC("aa:bb:cc:dd:ee:ff")))

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
	expectedAddr := net.ParseIP("2001:db8:1111:2222:3333:aabb:ccdd:eeff")
	if addr := iana.Options.OneAddress().IPv6Addr; !addr.Equal(expectedAddr) {
		t.Errorf("expected address %s, got %s", expectedAddr, addr)
	}
}

// TestHandler6_InvalidRelayMessages ensures that requests are dropped, if no
// address can be derived from the relay message.
func TestHandler6_InvalidRelayMessages(t *testing.T) {
	linkAddr := net.ParseIP("2001:db8:1111:2222:3333::")

	tests := []struct {
		name     string
		peerAddr net.IP
		noInner  bool
	}{
		{
			// no ff:fe in the middle, i.e. not derived from a 48-bit MAC
			name:     "peer address without EUI-48 MAC",
			peerAddr: net.ParseIP("fe80::a8bb:ccdd:eeff:1122"),
		},
		{
			name:     "peer address is not an IPv6 address",
			peerAddr: net.ParseIP("192.0.2.1"),
		},
		{
			name:     "relay message without encapsulated message",
			peerAddr: net.ParseIP("fe80::a8bb:ccff:fedd:eeff"),
			noInner:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var relayedRequest *dhcpv6.RelayMessage
			if tt.noInner {
				relayedRequest = &dhcpv6.RelayMessage{
					MessageType: dhcpv6.MessageTypeRelayForward,
					LinkAddr:    linkAddr,
					PeerAddr:    tt.peerAddr,
				}
			} else {
				req, err := dhcpv6.NewMessage()
				if err != nil {
					t.Fatal(err)
				}
				req.MessageType = dhcpv6.MessageTypeRequest
				req.AddOption(&dhcpv6.OptIANA{IaId: expectedIAID})

				relayedRequest, err = dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkAddr, tt.peerAddr)
				if err != nil {
					t.Fatal(err)
				}
			}

			stub, err := dhcpv6.NewMessage()
			if err != nil {
				t.Fatal(err)
			}
			stub.MessageType = dhcpv6.MessageTypeReply

			resp, stop := handler6(relayedRequest, stub)
			if resp != nil {
				t.Errorf("plugin should not return a message, got %v", resp)
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
