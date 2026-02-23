// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package data

import (
	"net"
	"os"
	"testing"

	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v3"

	"github.com/insomniacslk/dhcp/dhcpv6"
)

const (
	optionDisabled = iota
	optionEnabled
)

var (
	expectedIAID = [4]byte{1, 2, 3, 4}
)

func Init6() {
	data := api.DataConfig{}

	configData, _ := yaml.Marshal(data)

	file, _ := os.CreateTemp("", "config.yaml")
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

func TestWrongNumberArgs(t *testing.T) {
	_, err := setup6()
	if err == nil {
		t.Fatal("no error occurred when not providing a configuration file path, but it should have")
	}

	_, err = setup6("non-existing.yaml")
	if err == nil {
		t.Fatal("no error occurred when providing non existing configuration path, but it should have")
	}

	_, err = setup6("foo", "bar")
	if err == nil {
		t.Fatal("no error occurred when providing wrong number of args (2), but it should have")
	}
}

func TestIPAddressRequested6(t *testing.T) {
	Init6()

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{
		IaId: expectedIAID,
	})

	// MAC aa:bb:cc:dd:ee:ff -> EUI-64 modified: a8:bb:cc:ff:fe:dd:ee:ff
	// Embedded in link-local: fe80::a8bb:ccff:fedd:eeff
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
	if len(opts) != optionEnabled {
		t.Fatalf("Expected %d IANA option, got %d: %v", optionEnabled, len(opts), opts)
	}

	iana := resp.(*dhcpv6.Message).Options.OneIANA()
	preferred := iana.Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime
	valid := iana.Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime
	addr := iana.Options.OneAddress().IPv6Addr

	if preferred != preferredLifeTime {
		t.Errorf("Expected preferred life time %d, got %d", preferredLifeTime, preferred)
	}

	if valid != validLifeTime {
		t.Errorf("Expected valid life time %d, got %d", validLifeTime, valid)
	}

	if iana.IaId != expectedIAID {
		t.Errorf("expected IAID %d, got %d", expectedIAID, iana.IaId)
	}

	// 2001:db8:1111:2222:3333:: with MAC aa:bb:cc:dd:ee:ff in last 48 bits
	expectedIPAddress6 := net.IP{0x20, 0x01, 0x0d, 0xb8, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	if !addr.Equal(expectedIPAddress6) {
		t.Errorf("expected IPv6 address %v, got %v", expectedIPAddress6, addr)
	}
}

func TestIPAddressNotRequested6(t *testing.T) {
	Init6()

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
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d IANA option, got %d: %v", optionDisabled, len(opts), opts)
	}
}

func TestNoRelayDropped6(t *testing.T) {
	Init6()

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{
		IaId: expectedIAID,
	})

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := handler6(req, stub)
	if resp != nil {
		t.Fatal("plugin should not return a message")
	}
	if !stop {
		t.Error("plugin did not interrupt processing, but it should have")
	}
}

func TestLinkAddrNotAlignedTo80(t *testing.T) {
	Init6()

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{
		IaId: expectedIAID,
	})

	peerAddr := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0xa8, 0xbb, 0xcc, 0xff, 0xfe, 0xdd, 0xee, 0xff}
	// LinkAddr with non-zero bits in the last 48 bits -- not a valid /80 prefix
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
