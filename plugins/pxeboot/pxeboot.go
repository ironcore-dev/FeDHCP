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
//   - pxeboot: tftp://[2001:db8::dead]/pxe-file http://[2001:db8:a::1]/ipxe-file

package pxeboot

import (
	"fmt"
	"net/url"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var log = logger.GetLogger("plugins/pxeboot")

// Plugin wraps plugin registration information
var Plugin = plugins.Plugin{
	Name:   "pxeboot",
	Setup6: setup6,
	Setup4: setup4,
}

var (
	tftpOption, ipxeOption     dhcpv6.Option
	tftpOptionV4, ipxeOptionV4 dhcpv4.Option
)

func parseArgs(args ...string) (*url.URL, *url.URL, error) {
	if len(args) != 2 {
		return nil, nil, fmt.Errorf("exactly two arguments must be passed to PXEBOOT plugin, got %d", len(args))
	}
	tftp, err := url.Parse(args[0])
	if err != nil {
		return nil, nil, err
	}
	ipxe, err := url.Parse(args[1])
	if err != nil {
		return nil, nil, err
	}
	return tftp, ipxe, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	tftp, ipxe, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}
	tftpOption = dhcpv6.OptBootFileURL(tftp.String())
	ipxeOption = dhcpv6.OptBootFileURL(ipxe.String())

	log.Printf("loaded PXEBOOT plugin for DHCPv6.")
	return pxebootHandler6, nil
}

func setup4(args ...string) (handler.Handler4, error) {
	tftp, ipxe, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}
	tftpOptionV4 = dhcpv4.OptBootFileName(tftp.String())
	ipxeOptionV4 = dhcpv4.OptBootFileName(ipxe.String())

	log.Printf("loaded PXEBOOT plugin for DHCPv4.")
	return pxebootHandler4, nil
}

func pxebootHandler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	if tftpOption == nil || ipxeOption == nil {
		// nothing to do
		return resp, true
	}
	decap, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("Could not decapsulate request: %v", err)
		// drop the request, this is probably a critical error in the packet.
		return nil, true
	}

	if decap.IsOptionRequested(dhcpv6.OptionBootfileURL) {
		var opt *dhcpv6.Option

		// if TFTP request
		if decap.GetOneOption(dhcpv6.OptionClientArchType) != nil {
			optBytes := decap.GetOneOption(dhcpv6.OptionClientArchType).ToBytes()
			if len(optBytes) == 2 && optBytes[0] == 0 && optBytes[1] == 0x07 {
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
			log.Debugf("Added option %s", ipxeOption)
		}
	}

	return resp, true
}

func pxebootHandler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	if tftpOptionV4.Value == nil || ipxeOptionV4.Value == nil {
		// Nothing to do
		return resp, true
	}

	if req == nil {
		log.Error("Request is nil")
		return nil, false
	}

	// Check if boot file option is requested
	if req.IsOptionRequested(dhcpv4.OptionBootfileName) {
		var opt dhcpv4.Option

		// Check if it's a TFTP request
		if req.IsOptionRequested(dhcpv4.OptionParameterRequestList) {
			paramList := req.ParameterRequestList()
			for _, code := range paramList {
				if code == dhcpv4.OptionClientSystemArchitectureType {
					opt = tftpOptionV4
				}
			}
		}

		// Check if it's an iPXE request
		if len(req.UserClass()) > 0 {
			userClass := req.UserClass()
			if userClass[0] == "iPXE" {
				opt = ipxeOptionV4
			}
		}

		if opt.Code != nil {
			resp.UpdateOption(opt)
			log.Debugf("Added option %s", opt)
		}
	}

	return resp, true
}
