// Copyright 2018-present the CoreDHCP Authors. All rights reserved
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package pxeboot

import (
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
)

const (
	ipxePath = "http://[2001:db8::1]/boot.ipxe"
	tftpPath = "tftp://[2001:db8::1]/boot.efi"
)

var (
	numberOptsBootFileURL int
)

func Init(numOptBoot int) {
	numberOptsBootFileURL = numOptBoot

	_, err := setup6(tftpPath, ipxePath)
	if err != nil {
		log.Fatal(err)
	}
}

func TestPXERequested6(t *testing.T) {
	Init(1)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optUserClass := dhcpv6.OptUserClass{}
	buf := []byte{
		0, 4,
		'i', 'P', 'X', 'E',
	}
	_ = optUserClass.FromBytes(buf)
	req.UpdateOption(&optUserClass)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := pxeBootHandler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if !stop {
		t.Error("plugin does not interrupt processing, but it should have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}

	bootFileURL := resp.(*dhcpv6.Message).Options.BootFileURL()
	if bootFileURL != ipxePath {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, ipxePath)
	}
}

func TestTFTPRequested6(t *testing.T) {
	Init(1)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optClientArchType := dhcpv6.OptClientArchType(iana.EFI_X86_64)
	req.UpdateOption(optClientArchType)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := pxeBootHandler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if !stop {
		t.Error("plugin does not interrupt processing, but it should have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}

	bootFileURL := resp.(*dhcpv6.Message).Options.BootFileURL()
	if bootFileURL != tftpPath {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, tftpPath)
	}
}

func TestWrongPXERequested6(t *testing.T) {
	Init(0)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optUserClass := dhcpv6.OptUserClass{}
	buf := []byte{
		0, 6,
		'f', '0', '0', 'b', 'a', 'r', // nonsense
	}
	_ = optUserClass.FromBytes(buf)
	req.UpdateOption(&optUserClass)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := pxeBootHandler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if !stop {
		t.Error("plugin does not interrupt processing, but it should have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}
}

func TestWrongTFTPRequested6(t *testing.T) {
	Init(0)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optClientArchType := dhcpv6.OptClientArchType(iana.Arch(4711)) // nonsense
	req.UpdateOption(optClientArchType)

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := pxeBootHandler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if !stop {
		t.Error("plugin does not interrupt processing, but it should have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}
}

func TestPXENotRequested6(t *testing.T) {
	Init(0)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := pxeBootHandler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if !stop {
		t.Error("plugin does not interrupt processing, but it should have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}
}

func TestTFTPNotRequested6(t *testing.T) {
	Init(0)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))

	stub, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	stub.MessageType = dhcpv6.MessageTypeReply

	resp, stop := pxeBootHandler6(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if !stop {
		t.Error("plugin does not interrupt processing, but it should have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}
}
