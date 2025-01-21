// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

// Package nbp implements handling of an NBP (Network Boot Program) using an
// URL, e.g. http://[fe80::abcd:efff:fe12:3456]/my-nbp or tftp://10.0.0.1/my-nbp .
// The NBP information is only added if it is requested by the client.
//
// For DHCPv6 OPT_BOOTFILE_URL (option 59) is used, and the value is passed
// unmodified. If the query string is specified and contains a "param" key,
// its value is also passed as OPT_BOOTFILE_PARAM (option 60), so it will be
// duplicated between option 59 and 60.
//
// Example usage:
//
// server6:
//   - plugins:
//   - pxeboot: pxeboot_config.yaml

package pxeboot

import (
	"fmt"
	"net/url"
	"os"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v2"

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

var (
	tftpOption, ipxeOption                                       dhcpv6.Option
	tftpBootFileOption, tftpServerNameOption, ipxeBootFileOption *dhcpv4.Option
)

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.PxebootConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.PxebootConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	return config, nil
}

func parseConfig(args ...string) (*url.URL, *url.URL, error) {
	pxebootConfig, err := loadConfig(args...)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid configuration: %v", err)
	}

	tftp, err := url.Parse(pxebootConfig.TFTPServer)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid tftp url: %v", err)
	}

	ipxe, err := url.Parse(pxebootConfig.IPXEServer)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid ipxe url: %v", err)
	}

	if tftp.Scheme != "tftp" || tftp.Host == "" || tftp.Path == "" || tftp.Path[0] != '/' || tftp.Path[1:] == "" {
		return nil, nil, fmt.Errorf("malformed TFTP parameter, should be a valid URL")
	}

	if (ipxe.Scheme != "http" && ipxe.Scheme != "https") || ipxe.Host == "" || ipxe.Path == "" {
		return nil, nil, fmt.Errorf("malformed iPXE parameter, should be a valid URL")
	}

	return tftp, ipxe, nil
}

func setup4(args ...string) (handler.Handler4, error) {
	tftp, ipxe, err := parseConfig(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	opt1 := dhcpv4.OptBootFileName(tftp.Path[1:])
	tftpBootFileOption = &opt1

	opt2 := dhcpv4.OptTFTPServerName(tftp.Host)
	tftpServerNameOption = &opt2

	opt3 := dhcpv4.OptBootFileName(ipxe.String())
	ipxeBootFileOption = &opt3

	log.Printf("loaded PXEBOOT plugin for DHCPv4.")
	return pxeBootHandler4, nil
}

func pxeBootHandler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	log.Debugf("Received DHCPv4 request: %s", req.Summary())

	if tftpBootFileOption == nil || tftpServerNameOption == nil || ipxeBootFileOption == nil {
		// nothing to do
		return resp, false
	}

	if req.IsOptionRequested(dhcpv4.OptionBootfileName) {
		var opt, opt2 *dhcpv4.Option

		// if iPXE request
		if req.GetOneOption(dhcpv4.OptionUserClassInformation) != nil {
			userClassInfo := req.GetOneOption(dhcpv4.OptionUserClassInformation)
			log.Debugf("UserClassInformation: %s (%x)", string(userClassInfo), userClassInfo)
			if len(userClassInfo) >= 4 && string(userClassInfo[0:4]) == "iPXE" {
				opt = ipxeBootFileOption
			}
		} else
		// if TFTP request
		if req.GetOneOption(dhcpv4.OptionClassIdentifier) != nil {
			classID := req.GetOneOption(dhcpv4.OptionClassIdentifier)
			log.Debugf("ClassIdentifier: %s (%x)", string(classID), classID)
			if len(classID) >= 19 && string(classID[0:19]) == "PXEClient:Arch:0000" {
				opt = tftpBootFileOption
				opt2 = tftpServerNameOption
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

	log.Debugf("Sent DHCPv4 response: %s", resp.Summary())
	return resp, false
}

func setup6(args ...string) (handler.Handler6, error) {
	tftp, ipxe, err := parseConfig(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	tftpOption = dhcpv6.OptBootFileURL(tftp.String())
	ipxeOption = dhcpv6.OptBootFileURL(ipxe.String())

	log.Printf("loaded PXEBOOT plugin for DHCPv6.")
	return pxeBootHandler6, nil
}

func pxeBootHandler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())

	if tftpOption == nil || ipxeOption == nil {
		// nothing to do
		return resp, false
	}
	decap, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("Could not decapsulate request: %v", err)
		// drop the request, this is probably a critical error in the packet.
		return nil, false
	}

	if decap.IsOptionRequested(dhcpv6.OptionBootfileURL) {
		var opt *dhcpv6.Option

		// if TFTP request
		if decap.GetOneOption(dhcpv6.OptionClientArchType) != nil {
			optBytes := decap.GetOneOption(dhcpv6.OptionClientArchType).ToBytes()
			log.Debugf("ClientArchType: %s (%x)", string(optBytes), optBytes)
			if len(optBytes) == 2 && optBytes[0] == 0 && optBytes[1] == byte(iana.EFI_X86_64) { // 0x07
				opt = &tftpOption
			}
		}

		// if iPXE request
		if decap.GetOneOption(dhcpv6.OptionUserClass) != nil {
			userClass := decap.GetOneOption(dhcpv6.OptionUserClass).ToBytes()
			log.Debugf("UserClass: %s (%x)", string(userClass), userClass)
			if len(userClass) >= 5 && string(userClass[2:6]) == "iPXE" {
				opt = &ipxeOption
			}
		}

		if opt != nil {
			resp.AddOption(*opt)
			log.Debugf("Added option %s", *opt)
		}
	}

	log.Debugf("Sent DHCPv6 response: %s", resp.Summary())
	return resp, false
}
