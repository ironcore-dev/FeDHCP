// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package onmetal

import (
	"github.com/insomniacslk/dhcp/dhcpv6"
	"net"
	"testing"
)

const (
	optionDisabled = iota
	optionEnabled
	optionMultiple
)

var (
	expectedIPAddress6 = net.IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
	expectedIAID       = [4]byte{1, 2, 3, 4}
)

func Init6() {
	_, err := setup6()
	if err != nil {
		log.Fatal(err)
	}
}

/* parametrization */

func TestWrongNumberArgs(t *testing.T) {
	_, err := setup6("not-needed-arg")
	if err == nil {
		t.Fatal("no error occurred when providing wrong number of args (1), but it should have")
	}
}

/* IPv6 */
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

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, net.IPv6loopback)
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

	if !addr.Equal(expectedIPAddress6) {
		t.Errorf("expected IPv6 address %v, got %v", expectedIPAddress6, iana.Options.OneAddress().IPv6Addr)
	}
}

func TestIPAddressNotRequested6(t *testing.T) {
	Init6()

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, net.IPv6loopback)
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

func TestNoRelayIPAddressRequested6(t *testing.T) {
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
