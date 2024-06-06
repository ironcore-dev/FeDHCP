// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package httpboot

import (
	"fmt"
	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"net/url"
	"strings"
)

var bootFile4 string
var bootFile6 string

var log = logger.GetLogger("plugins/httpboot")

var Plugin = plugins.Plugin{
	Name:   "httpboot",
	Setup6: setup6,
	Setup4: setup4,
}

func parseArgs(args ...string) (*url.URL, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("Exactly one argument must be passed to the httpboot plugin, got %d", len(args))
	}
	return url.Parse(args[0])
}

func setup6(args ...string) (handler.Handler6, error) {
	u, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}
	bootFile6 = u.String()
	log.Printf("loaded httpboot plugin for DHCPv6.")
	return Handler6, nil
}

func setup4(args ...string) (handler.Handler4, error) {
	u, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}
	bootFile4 = u.String()
	log.Printf("loaded httpboot plugin for DHCPv4.")
	return Handler4, nil
}

func Handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())
	decap, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("Could not decapsulate request: %v", err)
		return nil, true
	}

	if decap.GetOneOption(dhcpv6.OptionVendorClass) != nil {
		vc := decap.GetOneOption(dhcpv6.OptionVendorClass).String()
		if strings.Contains(vc, "HTTPClient") {
			bf := &dhcpv6.OptionGeneric{
				OptionCode: dhcpv6.OptionBootfileURL,
				OptionData: []byte(bootFile6),
			}
			resp.AddOption(bf)
			vid := &dhcpv6.OptionGeneric{
				OptionCode: dhcpv6.OptionVendorClass,
				// 0000 (4 bytes) Enterprise Identifier
				// 0a (2 bytes) length of "HTTPClient"
				// - rest with HTTPClient
				OptionData: []byte("00000aHTTPClient"),
			}
			resp.AddOption(vid)
		}
	}

	return resp, false
}

func Handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	log.Debugf("Received DHCPv4 request: %s", req.Summary())
	if req.GetOneOption(dhcpv4.OptionClassIdentifier) != nil {
		vc := req.GetOneOption(dhcpv4.OptionClassIdentifier)
		if strings.Contains(string(vc), "HTTPClient") {
			bf := &dhcpv4.Option{
				Code:  dhcpv4.OptionBootfileName,
				Value: dhcpv4.String(bootFile4),
			}
			resp.Options.Update(*bf)
			vid := &dhcpv4.Option{
				Code:  dhcpv4.OptionClassIdentifier,
				Value: dhcpv4.String("HTTPClient"),
			}
			resp.Options.Update(*vid)
		}
	}
	return resp, false
}
