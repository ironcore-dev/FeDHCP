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
	expectedPrefix     = "2001:db8:1111:2222:3333::/80"
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
func TestPrefixDelegationRequested6(t *testing.T) {
	Init6()

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(&dhcpv6.OptIANA{
		IaId: expectedIAID,
	})
	req.AddOption(&dhcpv6.OptIAPD{
		IaId:    expectedIAID,
		T1:      preferredLifeTime,
		T2:      validLifeTime,
		Options: dhcpv6.PDOptions{},
	})

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), net.IPv6loopback)
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

	opts := resp.GetOption(dhcpv6.OptionIAPD)
	if len(opts) != optionEnabled {
		t.Fatalf("Expected %d IAPD option, got %d: %v", optionEnabled, len(opts), opts)
	}

	iapd := resp.(*dhcpv6.Message).Options.OneIAPD()
	t1 := iapd.T1
	t2 := iapd.T2
	pref := iapd.Options.Options[0].(*dhcpv6.OptIAPrefix).Prefix

	if t1 != preferredLifeTime {
		t.Errorf("Expected T1 %d, got %d", preferredLifeTime, t1)
	}

	if t2 != validLifeTime {
		t.Errorf("Expected T2 %d, got %d", validLifeTime, t2)
	}

	if iapd.IaId != expectedIAID {
		t.Errorf("expected IAID %d, got %d", expectedIAID, iapd.IaId)
	}

	if pref.String() != expectedPrefix {
		t.Errorf("expected prefix %v, got %v", expectedPrefix, pref)
	}
}
func TestPrefixDelegationNotRequested6(t *testing.T) {
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

	opts := resp.GetOption(dhcpv6.OptionIAPD)
	if len(opts) != optionDisabled {
		t.Fatalf("Expected %d IAPD option, got %d: %v", optionDisabled, len(opts), opts)
	}
}
