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
	IPXEOption  *dhcpv4.Option
}

type BootOptionsIPv6 struct {
	TFTPOptions map[api.Arch]dhcpv6.Option
	IPXEOption  dhcpv6.Option
}

var (
	BootOptsV4 *BootOptionsIPv4 = &BootOptionsIPv4{
		TFTPOptions: map[api.Arch]*TFTPOptionIPv4{},
	}
	BootOptsV6 *BootOptionsIPv6 = &BootOptionsIPv6{
		TFTPOptions: map[api.Arch]dhcpv6.Option{},
	}
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
	pxeBootConfig, err := loadConfig(args...)
	if err != nil {
		return err
	}

	for _, addr := range pxeBootConfig.IPXEAddress.IPv4 {
		ipxeAddress, err := url.Parse(addr)
		if err != nil {
			return err
		}
		if (ipxeAddress.Scheme != "http" && ipxeAddress.Scheme != "https") || ipxeAddress.Host == "" || ipxeAddress.Path == "" {
			return fmt.Errorf("malformed iPXE parameter, should be a valid URL")
		}
		bfn := dhcpv4.OptBootFileName(ipxeAddress.String())
		BootOptsV4.IPXEOption = &bfn
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
		BootOptsV4.TFTPOptions[arch] = &TFTPOptionIPv4{
			TFTPServerNameOption:   &sn,
			TFTPBootFileNameOption: &bfn,
		}
	}

	for _, addr := range pxeBootConfig.IPXEAddress.IPv6 {
		ipxeAddress, err := url.Parse(addr)
		if err != nil {
			return err
		}
		if (ipxeAddress.Scheme != "http" && ipxeAddress.Scheme != "https") || ipxeAddress.Host == "" || ipxeAddress.Path == "" {
			return fmt.Errorf("malformed iPXE parameter, should be a valid URL")
		}
		BootOptsV6.IPXEOption = dhcpv6.OptBootFileURL(ipxeAddress.String())
	}

	for arch, addr := range pxeBootConfig.TFTPAddress.IPv6 {
		tftpAddress, err := url.Parse(addr)
		if err != nil {
			return err
		}
		if tftpAddress.Scheme != "tftp" || tftpAddress.Host == "" || tftpAddress.Path == "" || tftpAddress.Path[0] != '/' || tftpAddress.Path[1:] == "" {
			return fmt.Errorf("malformed TFTP parameter, should be a valid URL")
		}

		BootOptsV6.TFTPOptions[arch] = dhcpv6.OptBootFileURL(tftpAddress.String())
	}

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
			if BootOptsV4.IPXEOption.String() == "" {
				log.Infof("No IPXE address configured for DHCPv4")
				return resp, false
			}
			opt = BootOptsV4.IPXEOption
		} else if tftp {
			switch arch {
			case api.AMD64:
				if BootOptsV4.TFTPOptions[api.AMD64].TFTPBootFileNameOption.String() == "" ||
					BootOptsV4.TFTPOptions[api.AMD64].TFTPServerNameOption.String() == "" {
					log.Infof("No TFTP address configured for DHCPv4")
					return resp, false
				}
				opt = BootOptsV4.TFTPOptions[api.AMD64].TFTPBootFileNameOption
				opt2 = BootOptsV4.TFTPOptions[api.AMD64].TFTPServerNameOption
			case api.ARM64:
				if BootOptsV4.TFTPOptions[api.ARM64].TFTPBootFileNameOption.String() == "" ||
					BootOptsV4.TFTPOptions[api.ARM64].TFTPServerNameOption.String() == "" {
					log.Infof("No TFTP option set for DHCPv4")
					return resp, false
				}
				opt = BootOptsV4.TFTPOptions[api.ARM64].TFTPBootFileNameOption
				opt2 = BootOptsV4.TFTPOptions[api.ARM64].TFTPServerNameOption
			default:
				log.Warnf("Unknown TFTP option set for DHCPv4")
			}
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

func setup6(args ...string) (handler.Handler6, error) {
	err := parseConfig(args...)
	if err != nil {
		return nil, err
	}

	log.Printf("loaded PXEBOOT plugin for DHCPv6.")
	return pxeBootHandler6, nil
}

func pxeBootHandler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
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
			if BootOptsV6.IPXEOption.String() == "" {
				log.Infof("No IPXE address configured for DHCPv6")
				return resp, false
			}
			opt = BootOptsV6.IPXEOption
		} else if tftp {
			switch arch {
			case api.AMD64:
				if BootOptsV6.TFTPOptions[api.AMD64].String() == "" {
					log.Infof("No TFTP address configured for DHCPv6 (%s)", arch)
					return resp, false
				}
				opt = BootOptsV6.TFTPOptions[api.AMD64]
			case api.ARM64:
				if BootOptsV6.TFTPOptions[api.ARM64].String() == "" {
					log.Infof("No TFTP address configured for DHCPv6 (%s)", arch)
					return resp, false
				}
				opt = BootOptsV6.TFTPOptions[api.ARM64]
			default:
				log.Warnf("Unknown TFTP option set for DHCPv6")
			}
		}

		if opt != nil {
			resp.AddOption(opt)
			log.Debugf("Added option %s", opt)
		}
	}

	return resp, false
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
