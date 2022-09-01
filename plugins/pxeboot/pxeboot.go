// Copyright 2018-present the CoreDHCP Authors. All rights reserved
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

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
//     - pxeboot: tftp://[2001:db8::dead]/pxe-file http://[2001:db8:a::1]/ipxe-file
//
package pxeboot

import (
	"fmt"
	"net/url"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var log = logger.GetLogger("plugins/pxeboot")

// Plugin wraps plugin registration information
var Plugin = plugins.Plugin{
	Name:   "pxeboot",
	Setup6: setup6,
}

var (
	tftpOption, ipxeOption dhcpv6.Option
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
