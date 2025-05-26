// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package onmetal

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/mdlayher/netx/eui64"

	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v3"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

var log = logger.GetLogger("plugins/onmetal")

var Plugin = plugins.Plugin{
	Name:   "onmetal",
	Setup6: setup6,
}

var prefixLength int

const (
	preferredLifeTime         = 24 * time.Hour
	validLifeTime             = 24 * time.Hour
	prefixDelegationLengthMin = 1
	prefixDelegationLengthMax = 127
)

// args[0] = path to config file
func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.OnMetalConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.OnMetalConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	onMetalConfig, err := loadConfig(args...)
	if err != nil {
		return nil, err
	}

	prefixLength = onMetalConfig.PrefixDelegation.Length
	if prefixLength < prefixDelegationLengthMin || prefixLength > prefixDelegationLengthMax {
		return nil, fmt.Errorf("invalid prefix length: %d", prefixLength)
	}

	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("Received DHCPv6 request: %s", req.Summary())

	if !req.IsRelay() {
		log.Printf("Received non-relay DHCPv6 request. Dropping.")
		return nil, true
	}

	relayMsg := req.(*dhcpv6.RelayMessage)

	ipaddr := make(net.IP, len(relayMsg.LinkAddr))
	copy(ipaddr, relayMsg.LinkAddr)
	ipaddr[len(ipaddr)-1] += 1

	m, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("BUG: could not decapsulate: %v", err)
		return nil, true
	}

	if m.Options.OneIANA() == nil {
		log.Debug("No address requested")
		return resp, false
	}

	// Retrieve IPv6 prefix and MAC address from IPv6 address
	_, mac, err := eui64.ParseIP(relayMsg.PeerAddr)
	if err != nil {
		log.Errorf("Could not parse peer address: %s", err)
		return nil, true
	}
	macKey := strings.ReplaceAll(mac.String(), ":", "")

	iana := &dhcpv6.OptIANA{
		IaId: m.Options.OneIANA().IaId,
		Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
			&dhcpv6.OptIAAddress{
				IPv6Addr:          ipaddr,
				PreferredLifetime: preferredLifeTime,
				ValidLifetime:     validLifeTime,
			},
		}},
	}
	resp.AddOption(iana)
	log.Infof("Client %s: added option IA address %s", macKey, iana.String())

	optIAPD := m.Options.OneIAPD()
	T1 := preferredLifeTime
	T2 := validLifeTime
	var mask80 = net.CIDRMask(prefixLength, 128)

	if optIAPD != nil {
		if optIAPD.T1 != 0 {
			T1 = optIAPD.T1
		}
		if optIAPD.T2 != 0 {
			T2 = optIAPD.T2
		}
		iapd := &dhcpv6.OptIAPD{
			IaId: optIAPD.IaId,
			T1:   T1,
			T2:   T2,
			Options: dhcpv6.PDOptions{Options: dhcpv6.Options{&dhcpv6.OptIAPrefix{
				PreferredLifetime: preferredLifeTime,
				ValidLifetime:     validLifeTime,
				Prefix: &net.IPNet{
					Mask: mask80,
					IP:   ipaddr.Mask(mask80),
				},
				Options: dhcpv6.PrefixOptions{Options: dhcpv6.Options{}},
			}}},
		}
		resp.UpdateOption(iapd)
		log.Infof("Client %s, added option IA prefix %s", macKey, iapd.String())
	}

	log.Debugf("Sent DHCPv6 response: %s", resp.Summary())

	return resp, false
}
