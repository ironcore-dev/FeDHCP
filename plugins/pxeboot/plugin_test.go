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
	tftpAMDPath4 = "tftp://192.168.0.1/x86_64/amd64.efi"
	tftpARMPath4 = "tftp://192.168.0.1/aarch64/arm64.efi"
	tftpAMDPath6 = "tftp://[2001:db8::1]/x86_64/amd64.efi"
	tftpARMPath6 = "tftp://[2001:db8::1]/aarch64/arm64.efi"
	ipxePath4    = "http://192.168.0.2/ipxe/boot4.pxe"
	ipxePath6    = "http://[2001:db8::2]/ipxe/boot6.pxe"
)

var (
	numberOptsBootFileURL int
	tempConfigFilePattern = "*-pxeboot_config.yaml"
	validConfig           = &api.PxeBootConfig{
		TFTPAddress: api.Addresses{
			IPv4: map[api.Arch]string{
				api.AMD64: tftpAMDPath4,
				api.ARM64: tftpARMPath4,
			},
			IPv6: map[api.Arch]string{
				api.AMD64: tftpAMDPath6,
				api.ARM64: tftpARMPath6,
			},
		},
		IPXEAddress: api.Addresses{
			IPv4: map[api.Arch]string{
				api.AMD64: ipxePath4,
				api.ARM64: ipxePath4,
			},
			IPv6: map[api.Arch]string{
				api.AMD64: ipxePath6,
				api.ARM64: ipxePath6,
			},
		},
	}
)

func createTempConfig(config api.PxeBootConfig, tempDir string) (string, error) {
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

func Init4(config api.PxeBootConfig, tempDir string) error {
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

func Init6(config api.PxeBootConfig, tempDir string, numOptBoot int) error {
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

func TestWrongArgs4(t *testing.T) {
	malformedTFTPPath := []string{"tftp://example.com", "tftp:/example.com/boot.efi", "foo://example.com/boot.efi"}
	malformedIPXEPath := []string{"https://example.com", "http:/www.example.com/boot.ipxe", "foo://example.com/boot.ipxe"}
	validConfig := api.PxeBootConfig{
		TFTPAddress: api.Addresses{
			IPv4: map[api.Arch]string{
				api.AMD64: tftpAMDPath4,
				api.ARM64: tftpARMPath4,
			},
			IPv6: map[api.Arch]string{
				api.AMD64: tftpAMDPath6,
				api.ARM64: tftpARMPath6,
			},
		},
		IPXEAddress: api.Addresses{
			IPv4: map[api.Arch]string{
				api.AMD64: ipxePath4,
				api.ARM64: ipxePath4,
			},
			IPv6: map[api.Arch]string{
				api.AMD64: ipxePath6,
				api.ARM64: ipxePath6,
			},
		},
	}
	for _, wrongTFTP := range malformedTFTPPath {
		for _, arch := range []api.Arch{api.AMD64, api.ARM64} {
			config := validConfig
			config.TFTPAddress.IPv4[arch] = wrongTFTP
			tempDir := t.TempDir()
			err := Init4(config, tempDir)
			if err == nil {
				t.Fatalf("no error occurred when providing wrong TFTP path %s for arch %s, but it should have", wrongTFTP, arch)
			}

			tftpo, exists := BootOptsV4.TFTPOptions[arch]
			if exists && tftpo.TFTPBootFileNameOption != nil {
				t.Fatalf("TFTP boot file was set when providing wrong TFTP path %s for arch %s, but it should be empty", wrongTFTP, arch)
			}

			if exists && tftpo.TFTPServerNameOption != nil {
				t.Fatalf("TFTP server name was set when providing wrong TFTP path %s for arch %s, but it should be empty", wrongTFTP, arch)
			}
		}
	}

	for _, wrongIPXE := range malformedIPXEPath {
		for _, arch := range []api.Arch{api.AMD64, api.ARM64} {
			config := validConfig
			config.IPXEAddress.IPv4[arch] = wrongIPXE
			tempDir := t.TempDir()
			err := Init4(config, tempDir)
			if err == nil {
				t.Fatalf("no error occurred when providing wrong IPXE path %s for arch %s, but it should have", wrongIPXE, arch)
			}

			if BootOptsV4.IPXEOption != nil {
				t.Fatalf("IPXE boot file was set when providing wrong IPXE path %s for arch %s, but it should be empty", wrongIPXE, arch)
			}
		}
	}

	/*
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
	*/
}

/*
func injectWrongParameter(origMap map[api.Arch]string, arch api.Arch, tftp string) api.PxeBootConfig {

	for k, v := range origMap{
		if
	}
}
*/

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
	if bootFileURL != ipxePath6 {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, ipxePath6)
	}
}

func TestTFTPRequestedAMD6(t *testing.T) {
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
	if bootFileURL != tftpAMDPath6 {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, tftpAMDPath6)
	}
}

func TestTFTPRequestedARM6(t *testing.T) {
	tempDir := t.TempDir()
	_ = Init6(*validConfig, tempDir, 1)

	req, err := dhcpv6.NewMessage()
	if err != nil {
		t.Fatal(err)
	}
	req.MessageType = dhcpv6.MessageTypeRequest
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))
	optClientArchType := dhcpv6.OptClientArchType(iana.EFI_ARM64)
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
	if bootFileURL != tftpARMPath6 {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, tftpARMPath6)
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
	if bootFileURL != ipxePath4 {
		t.Errorf("Found BootFileURL %s, expected %s", bootFileURL, ipxePath4)
	}
}

func TestTFTPRequestedAMD4(t *testing.T) {
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
	if combinedPath != tftpAMDPath4 {
		t.Errorf("Found TFTP path %s, expected %s", combinedPath, tftpAMDPath4)
	}
}

func TestTFTPRequestedARM4(t *testing.T) {
	tempDir := t.TempDir()
	_ = Init4(*validConfig, tempDir)

	req, err := dhcpv4.NewDiscovery(net.HardwareAddr{
		0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
		dhcpv4.WithRequestedOptions(dhcpv4.OptionBootfileName),
	)
	if err != nil {
		t.Fatal(err)
	}

	optClassID := dhcpv4.OptClassIdentifier("PXEClient:Arch:00011")
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
	if combinedPath != tftpARMPath4 {
		t.Errorf("Found TFTP path %s, expected %s", combinedPath, tftpARMPath4)
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
