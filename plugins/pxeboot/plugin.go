// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package pxeboot

import (
	"fmt"
	"net/url"
	"os"

	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/ironcore-dev/fedhcp/internal/printer"
	"gopkg.in/yaml.v3"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var log = logger.GetLogger("plugins/pxeboot")

// Plugin wraps plugin registration information
var Plugin = plugins.Plugin{
	Name:   "pxeboot",
	Setup4: setup4,
	Setup6: setup6,
}

type TFTPOptionIPv4 struct {
	TFTPServerNameOption   *dhcpv4.Option
	TFTPBootFileNameOption *dhcpv4.Option
}

type BootOptionsIPv4 struct {
	TFTPOptions map[api.Arch]*TFTPOptionIPv4
	IPXEOptions map[api.Arch]*dhcpv4.Option
}

type BootOptionsIPv6 struct {
	TFTPOptions map[api.Arch]dhcpv6.Option
	IPXEOptions map[api.Arch]dhcpv6.Option
}

var (
	bootOptsV4 *BootOptionsIPv4
	bootOptsV6 *BootOptionsIPv6
)

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.PxeBootConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.PxeBootConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	return config, nil
}

func parseConfig(args ...string) error {
	bootOptsV4 = &BootOptionsIPv4{
		TFTPOptions: map[api.Arch]*TFTPOptionIPv4{},
		IPXEOptions: map[api.Arch]*dhcpv4.Option{},
	}
	bootOptsV6 = &BootOptionsIPv6{
		TFTPOptions: map[api.Arch]dhcpv6.Option{},
		IPXEOptions: map[api.Arch]dhcpv6.Option{},
	}

	pxeBootConfig, err := loadConfig(args...)
	if err != nil {
		return err
	}

	for arch, addr := range pxeBootConfig.IPXEAddress.IPv4 {
		ipxeAddress, err := url.Parse(addr)
		if err != nil {
			return err
		}
		if (ipxeAddress.Scheme != "http" && ipxeAddress.Scheme != "https") || ipxeAddress.Host == "" || ipxeAddress.Path == "" {
			return fmt.Errorf("malformed iPXE parameter, should be a valid URL")
		}
		bfn := dhcpv4.OptBootFileName(ipxeAddress.String())
		bootOptsV4.IPXEOptions[arch] = &bfn
	}

	for arch, addr := range pxeBootConfig.TFTPAddress.IPv4 {
		tftpAddress, err := url.Parse(addr)
		if err != nil {
			return err
		}
		if tftpAddress.Scheme != "tftp" || tftpAddress.Host == "" || tftpAddress.Path == "" || tftpAddress.Path[0] != '/' || tftpAddress.Path[1:] == "" {
			return fmt.Errorf("malformed TFTP parameter, should be a valid URL")
		}

		sn := dhcpv4.OptTFTPServerName(tftpAddress.Host)
		bfn := dhcpv4.OptBootFileName(tftpAddress.Path[1:])
		bootOptsV4.TFTPOptions[arch] = &TFTPOptionIPv4{
			TFTPServerNameOption:   &sn,
			TFTPBootFileNameOption: &bfn,
		}
	}

	log.Debugf("before parse config: %v", bootOptsV6)
	for arch, addr := range pxeBootConfig.IPXEAddress.IPv6 {
		ipxeAddress, err := url.Parse(addr)
		if err != nil {
			return err
		}
		if (ipxeAddress.Scheme != "http" && ipxeAddress.Scheme != "https") || ipxeAddress.Host == "" || ipxeAddress.Path == "" {
			return fmt.Errorf("malformed iPXE parameter, should be a valid URL")
		}
		bootOptsV6.IPXEOptions[arch] = dhcpv6.OptBootFileURL(ipxeAddress.String())
	}

	for arch, addr := range pxeBootConfig.TFTPAddress.IPv6 {
		tftpAddress, err := url.Parse(addr)
		if err != nil {
			return err
		}
		if tftpAddress.Scheme != "tftp" || tftpAddress.Host == "" || tftpAddress.Path == "" || tftpAddress.Path[0] != '/' || tftpAddress.Path[1:] == "" {
			return fmt.Errorf("malformed TFTP parameter, should be a valid URL")
		}

		bootOptsV6.TFTPOptions[arch] = dhcpv6.OptBootFileURL(tftpAddress.String())
	}
	log.Debugf("after parse config: %v", bootOptsV6)

	return nil
}

func setup4(args ...string) (handler.Handler4, error) {
	if err := parseConfig(args...); err != nil {
		return nil, err
	}

	log.Printf("loaded PXEBOOT plugin for DHCPv4.")
	return pxeBootHandler4, nil
}

func pxeBootHandler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	if req == nil {
		log.Error("Received nil IPv4 request")
		return nil, true
	}

	printer.VerboseRequest(req, log, printer.IPv4)
	defer printer.VerboseResponse(req, resp, log, printer.IPv4)

	if req.IsOptionRequested(dhcpv4.OptionBootfileName) {
		var opt, opt2 *dhcpv4.Option

		tftp, arch := isTFTPRequested4(req)
		ipxe := isIPXERequested4(req)

		if ipxe {
			if !checkIPXEOptionsV4ForArchAreValid(arch) {
				log.Infof("No IPXE address configured for DHCPv4")
				return resp, false
			}
			opt = bootOptsV4.IPXEOptions[arch]
		} else if tftp {
			if !checkTFTPOptionsV4ForArchAreValid(arch) {
				log.Infof("No TFTP address configured for DHCPv4")
				return resp, false
			}
			opt = bootOptsV4.TFTPOptions[arch].TFTPBootFileNameOption
			opt2 = bootOptsV4.TFTPOptions[arch].TFTPServerNameOption
		}

		if opt != nil {
			resp.Options.Update(*opt)
			log.Debugf("Added option %s", *opt)
		}
		if opt2 != nil {
			resp.Options.Update(*opt2)
			log.Debugf("Added option %s", *opt2)
		}
	}

	return resp, false
}

func checkTFTPOptionsV4ForArchAreValid(arch api.Arch) bool {
	if bootOptsV4.TFTPOptions == nil {
		return false
	}

	v, exists := bootOptsV4.TFTPOptions[arch]
	if !exists {
		return false
	}

	if v.TFTPServerNameOption == nil ||
		v.TFTPBootFileNameOption.String() == "" ||
		v.TFTPBootFileNameOption == nil ||
		v.TFTPBootFileNameOption.String() == "" {
		return false
	}

	return true
}

func checkIPXEOptionsV4ForArchAreValid(arch api.Arch) bool {
	if bootOptsV4.IPXEOptions == nil {
		return false
	}

	v, exists := bootOptsV4.IPXEOptions[arch]
	if !exists || v.String() == "" {
		return false
	}

	return true
}

func isTFTPRequested4(req *dhcpv4.DHCPv4) (bool, api.Arch) {
	if req.GetOneOption(dhcpv4.OptionClassIdentifier) != nil {
		classID := req.GetOneOption(dhcpv4.OptionClassIdentifier)
		log.Debugf("ClassIdentifier: %s (%x)", string(classID), classID)
		if len(classID) >= 20 && string(classID[0:19]) == "PXEClient:Arch:0000" {
			return true, api.AMD64
		} else if len(classID) >= 20 && string(classID[0:20]) == "PXEClient:Arch:00011" {
			return true, api.ARM64
		} else {
			return true, api.UnknownArch
		}
	}
	return false, api.UnknownArch
}

func isIPXERequested4(req *dhcpv4.DHCPv4) bool {
	if req.GetOneOption(dhcpv4.OptionUserClassInformation) != nil {
		userClassInfo := req.GetOneOption(dhcpv4.OptionUserClassInformation)
		log.Debugf("UserClassInformation: %s (%x)", string(userClassInfo), userClassInfo)
		if len(userClassInfo) >= 4 && string(userClassInfo[0:4]) == "iPXE" {
			return true
		} else {
			log.Warnf("Non-IPXE UserClass option set for DHCPv4")
		}
	}

	return false
}

func setup6(args ...string) (handler.Handler6, error) {
	if err := parseConfig(args...); err != nil {
		return nil, err
	}

	log.Printf("loaded PXEBOOT plugin for DHCPv6.")
	return pxeBootHandler6, nil
}

func pxeBootHandler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Handler: %v", bootOptsV6)
	if req == nil {
		log.Error("Received nil IPv6 request")
		return nil, true
	}

	printer.VerboseRequest(req, log, printer.IPv6)
	defer printer.VerboseResponse(req, resp, log, printer.IPv6)

	decap, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("Could not decapsulate request: %v", err)
		// drop the request, this is probably a critical error in the packet.
		return nil, false
	}

	if decap.IsOptionRequested(dhcpv6.OptionBootfileURL) {
		var opt dhcpv6.Option

		tftp, arch := isTFTPRequested6(decap)
		ipxe := isIPXERequested6(decap)

		if ipxe {
			if !checkIPXEOptionsV6ForArchAreValid(arch) {
				log.Infof("No IPXE address configured for DHCPv6")
				return resp, false
			}
			opt = bootOptsV6.IPXEOptions[arch]
		} else if tftp {
			if !checkTFTPOptionsV6ForArchAreValid(arch) {
				log.Infof("No TFTP address configured for DHCPv6")
				return resp, false
			}
			opt = bootOptsV6.TFTPOptions[arch]
		}

		if opt != nil {
			resp.AddOption(opt)
			log.Debugf("Added option %s", opt)
		}
	}

	return resp, false
}

func isTFTPRequested6(req *dhcpv6.Message) (bool, api.Arch) {
	if req.GetOneOption(dhcpv6.OptionClientArchType) != nil {
		optBytes := req.GetOneOption(dhcpv6.OptionClientArchType).ToBytes()
		log.Debugf("ClientArchType: %s (%x)", string(optBytes), optBytes)
		if len(optBytes) == 2 && optBytes[0] == 0 && optBytes[1] == byte(iana.EFI_X86_64) { // 0x07
			return true, api.AMD64
		} else if len(optBytes) == 2 && optBytes[0] == 0 && optBytes[1] == byte(iana.EFI_ARM64) { // 0x0B
			return true, api.ARM64
		} else {
			return true, api.UnknownArch
		}
	}
	return false, api.UnknownArch

}

func isIPXERequested6(req *dhcpv6.Message) bool {
	if req.GetOneOption(dhcpv6.OptionUserClass) != nil {
		userClass := req.GetOneOption(dhcpv6.OptionUserClass).ToBytes()
		log.Debugf("UserClass: %s (%x)", string(userClass), userClass)
		if len(userClass) >= 5 && string(userClass[2:6]) == "iPXE" {
			return true
		} else {
			log.Warnf("Non-IPXE UserClass option set for DHCPv6")
			return false
		}
	}
	return false
}

func checkTFTPOptionsV6ForArchAreValid(arch api.Arch) bool {
	log.Debugf("Checking TFTP options: %v", bootOptsV6)
	if bootOptsV6.TFTPOptions == nil {
		return false
	}

	v, exists := bootOptsV6.TFTPOptions[arch]
	if !exists || v.String() == "" {
		return false
	}

	return true
}

func checkIPXEOptionsV6ForArchAreValid(arch api.Arch) bool {
	if bootOptsV6.IPXEOptions == nil {
		return false
	}

	v, exists := bootOptsV6.IPXEOptions[arch]
	if !exists || v.String() == "" {
		return false
	}

	return true
}
