// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package pxeboot

import (
	"net"
	"net/url"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"

	"github.com/insomniacslk/dhcp/dhcpv6"
)

const (
	ipxePath = "http://[2001:db8::1]/boot.ipxe"
	tftpPath = "tftp://[2001:db8::1]/boot.efi"
)

var (
	numberOptsBootFileURL int
)

func Init4() {
	_, err := setup4(tftpPath, ipxePath)
	if err != nil {
		log.Fatal(err)
	}
}

func Init6(numOptBoot int) {
	numberOptsBootFileURL = numOptBoot

	_, err := setup6(tftpPath, ipxePath)
	if err != nil {
		log.Fatal(err)
	}
}

/* parametrization */

func TestWrongNumberArgs(t *testing.T) {
	_, _, err := parseArgs(tftpPath, ipxePath, "not-needed-arg")
	if err == nil {
		t.Fatal("no error occurred when providing wrong number of args (3), but it should have")
	}

	_, _, err = parseArgs("only-one-arg")
	if err == nil {
		t.Fatal("no error occurred when providing wrong number of args (1), but it should have")
	}
}

func TestWrongArgs(t *testing.T) {
	malformedTFTPPath := []string{"tftp://1.2.3.4/", "foo://1.2.3.4/boot.efi"}
	malformedIPXEPath := []string{"httpfoo://www.example.com", "https:/1.2.3"}

	for _, wrongTFTP := range malformedTFTPPath {
		_, err := setup4(wrongTFTP, ipxePath)
		if err == nil {
			t.Fatalf("no error occurred when providing wrong TFTP path %s, but it should have", wrongTFTP)
		}
		if tftpBootFileOption != nil {
			t.Fatalf("TFTP boot file was set when providing wrong TFTP path %s, but it should be empty", wrongTFTP)
		}
		if tftpServerNameOption != nil {
			t.Fatalf("TFTP server name was set when providing wrong TFTP path %s, but it should be empty", wrongTFTP)
		}
		if ipxeBootFileOption != nil {
			t.Fatalf("IPXE boot file was set when providing wrong TFTP path %s, but it should be empty", wrongTFTP)
		}

		_, err = setup6(wrongTFTP, ipxePath)
		if err == nil {
			t.Fatalf("no error occurred when providing wrong TFTP path %s, but it should have", wrongTFTP)
		}
		if tftpOption != nil {
			t.Fatalf("TFTP boot file was set when providing wrong TFTP path %s, but it should be empty", wrongTFTP)
		}
		if ipxeOption != nil {
			t.Fatalf("IPXE boot file was set when providing wrong TFTP path %s, but it should be empty", wrongTFTP)
		}
	}

	for _, wrongIPXE := range malformedIPXEPath {
		_, err := setup4(tftpPath, wrongIPXE)
		if err == nil {
			t.Fatalf("no error occurred when providing wrong IPXE path %s, but it should have", wrongIPXE)
		}
		if tftpBootFileOption != nil {
			t.Fatalf("TFTP boot file was set when providing wrong IPXE path %s, but it should be empty", wrongIPXE)
		}
		if tftpServerNameOption != nil {
			t.Fatalf("TFTP server name set when providing wrong IPXE path %s, but it should be empty", wrongIPXE)
		}
		if ipxeBootFileOption != nil {
			t.Fatalf("IPXE boot file was set when providing wrong IPXE path %s, but it should be empty", wrongIPXE)
		}

		_, err = setup6(tftpPath, wrongIPXE)
		if err == nil {
			t.Fatalf("no error occurred when providing wrong IPXE path %s, but it should have", wrongIPXE)
		}
		if tftpOption != nil {
			t.Fatalf("TFTP boot file was set when providing wrong IPXE path %s, but it should be empty", wrongIPXE)
		}
		if ipxeOption != nil {
			t.Fatalf("IPXE boot file was set when providing wrong IPXE path %s, but it should be empty", wrongIPXE)
		}
	}
}

/* IPv6 */

func TestPXERequested6(t *testing.T) {
	Init6(1)

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

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
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
	Init6(1)

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

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
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
	Init6(0)

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

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}
}

func TestWrongTFTPRequested6(t *testing.T) {
	Init6(0)

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

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}
}

func TestPXENotRequested6(t *testing.T) {
	Init6(0)

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

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}
}

func TestTFTPNotRequested6(t *testing.T) {
	Init6(0)

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

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}
	opts := resp.GetOption(dhcpv6.OptionBootfileURL)
	if len(opts) != numberOptsBootFileURL {
		t.Fatalf("Expected %d BootFileUrl option, got %d: %v", numberOptsBootFileURL, len(opts), opts)
	}
}

/* IPV4 */

func TestPXERequested4(t *testing.T) {
	Init4()

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionBootfileName),
	)
	if err != nil {
		t.Fatal(err)
	}

	optUserClass := dhcpv4.OptUserClass("iPXE")
	req.UpdateOption(optUserClass)

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := pxeBootHandler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	bootFileURL := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileURL != ipxePath {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, ipxePath)
	}
}

func TestTFTPRequested4(t *testing.T) {
	Init4()

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionBootfileName),
	)
	if err != nil {
		t.Fatal(err)
	}

	optClassID := dhcpv4.OptClassIdentifier("PXEClient:Arch:00007")
	req.UpdateOption(optClassID)

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := pxeBootHandler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	const protocol = "tftp"
	tftpServerName := dhcpv4.GetString(dhcpv4.OptionTFTPServerName, resp.Options)
	bootFileName := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	combinedPath := (&url.URL{
		Scheme: protocol,
		Host:   tftpServerName,
		Path:   bootFileName,
	}).String()
	if combinedPath != tftpPath {
		t.Errorf("Found TFTP path %s, expected %s", combinedPath, tftpPath)
	}
}

func TestPXENotRequested4(t *testing.T) {
	Init4()

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionBootfileName),
	)
	if err != nil {
		t.Fatal(err)
	}

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := pxeBootHandler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	bootFileURL := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileURL != "" {
		t.Errorf("Found BootFileURL %s, expected empty", bootFileURL)
	}
}

func TestTFTPNotRequested4(t *testing.T) {
	Init4()

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionBootfileName),
	)
	if err != nil {
		t.Fatal(err)
	}

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := pxeBootHandler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	tftpServerName := dhcpv4.GetString(dhcpv4.OptionTFTPServerName, resp.Options)
	if tftpServerName != "" {
		t.Errorf("Found TFTP server %s, expected empty", tftpServerName)
	}
	bootFileName := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileName != "" {
		t.Errorf("Found TFTP path %s, expected empty", bootFileName)
	}
}

func TestWrongPXERequested4(t *testing.T) {
	Init4()

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionBootfileName),
	)
	if err != nil {
		t.Fatal(err)
	}

	optUserClass := dhcpv4.OptUserClass("foobar") // nonsense
	req.UpdateOption(optUserClass)

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := pxeBootHandler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	bootFileURL := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileURL != "" {
		t.Errorf("Found BootFileURL %s, expected empty", bootFileURL)
	}
}

func TestWrongTFTPRequested4(t *testing.T) {
	Init4()

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionBootfileName),
	)
	if err != nil {
		t.Fatal(err)
	}

	optClassID := dhcpv4.OptClassIdentifier("foobar") // nonsense
	req.UpdateOption(optClassID)

	stub, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	resp, stop := pxeBootHandler4(req, stub)
	if resp == nil {
		t.Fatal("plugin did not return a message")
	}

	if stop {
		t.Error("plugin interrupted processing, but it shouldn't have")
	}

	tftpServerName := dhcpv4.GetString(dhcpv4.OptionTFTPServerName, resp.Options)
	if tftpServerName != "" {
		t.Errorf("Found TFTP server %s, expected empty", tftpServerName)
	}
	bootFileName := dhcpv4.GetString(dhcpv4.OptionBootfileName, resp.Options)
	if bootFileName != "" {
		t.Errorf("Found TFTP path %s, expected empty", bootFileName)
	}
}
