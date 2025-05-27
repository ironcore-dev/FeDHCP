// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package pxeboot

import (
	"net"
	"net/url"
	"os"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"

	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v3"
)

const (
	ipxePath = "http://[2001:db8::1]/boot.ipxe"
	tftpPath = "tftp://[2001:db8::1]/boot.efi"
)

var (
	numberOptsBootFileURL int
	tempConfigFilePattern = "*-pxeboot_config.yaml"
	validConfig           = &api.PxebootConfig{
		TFTPServer: tftpPath,
		IPXEServer: ipxePath,
	}
)

func createTempConfig(config api.PxebootConfig, tempDir string) (string, error) {
	configData, err := yaml.Marshal(config)
	if err != nil {
		return "", err
	}

	file, err := os.CreateTemp(tempDir, tempConfigFilePattern)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	configFile := file.Name()

	err = os.WriteFile(configFile, configData, 0644)
	if err != nil {
		return "", err
	}

	return configFile, nil
}

func Init4(config api.PxebootConfig, tempDir string) error {
	configFile, err := createTempConfig(config, tempDir)
	if err != nil {
		return err
	}

	_, err = setup4(configFile)
	if err != nil {
		return err
	}

	return nil
}

func Init6(config api.PxebootConfig, tempDir string, numOptBoot int) error {
	numberOptsBootFileURL = numOptBoot

	configFile, err := createTempConfig(config, tempDir)
	if err != nil {
		return err
	}

	_, err = setup6(configFile)
	if err != nil {
		return err
	}

	return err
}

/* parametrization */

func TestWrongNumberArgs(t *testing.T) {
	_, err := parseArgs("foo", "bar")
	if err == nil {
		t.Fatal("no error occurred when providing wrong number of args (2), but it should have")
	}

	_, err = parseArgs()
	if err == nil {
		t.Fatal("no error occurred when providing wrong number of args (0), but it should have")
	}
}

func TestWrongArgs(t *testing.T) {
	malformedTFTPPath := []string{"tftp://example.com", "tftp:/example.com/boot.efi", "foo://example.com/boot.efi"}
	for _, wrongTFTP := range malformedTFTPPath {
		config := &api.PxebootConfig{
			TFTPServer: wrongTFTP,
			IPXEServer: ipxePath,
		}
		tempDir := t.TempDir()
		err := Init4(*config, tempDir)
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

		err = Init6(*config, tempDir, 0)
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

	malformedIPXEPath := []string{"https://example.com", "http:/www.example.com/boot.ipxe", "foo://example.com/boot.ipxe"}
	for _, wrongIPXE := range malformedIPXEPath {
		config := &api.PxebootConfig{
			TFTPServer: tftpPath,
			IPXEServer: wrongIPXE,
		}
		tempDir := t.TempDir()
		err := Init4(*config, tempDir)
		if err == nil {
			t.Fatalf("no error occurred when providing wrong IPXE path %s, but it should have", wrongIPXE)
		}
		err = Init4(*config, tempDir)
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

		err = Init6(*config, tempDir, 0)
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
	tempDir := t.TempDir()
	_ = Init6(*validConfig, tempDir, 1)

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
	tempDir := t.TempDir()
	_ = Init6(*validConfig, tempDir, 1)

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
	tempDir := t.TempDir()
	_ = Init6(*validConfig, tempDir, 0)

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
	tempDir := t.TempDir()
	_ = Init6(*validConfig, tempDir, 0)

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
	tempDir := t.TempDir()
	_ = Init6(*validConfig, tempDir, 0)

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
	tempDir := t.TempDir()
	_ = Init6(*validConfig, tempDir, 0)

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
	tempDir := t.TempDir()
	_ = Init4(*validConfig, tempDir)

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
	tempDir := t.TempDir()
	_ = Init4(*validConfig, tempDir)

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
	tempDir := t.TempDir()
	_ = Init4(*validConfig, tempDir)

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
	tempDir := t.TempDir()
	_ = Init4(*validConfig, tempDir)

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
	tempDir := t.TempDir()
	_ = Init4(*validConfig, tempDir)

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
	tempDir := t.TempDir()
	_ = Init4(*validConfig, tempDir)

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
