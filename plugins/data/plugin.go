// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package data

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/ironcore-dev/fedhcp/internal/helper"
	"github.com/ironcore-dev/fedhcp/internal/printer"
	"gopkg.in/yaml.v3"
)

var log = logger.GetLogger("plugins/data")

var Plugin = plugins.Plugin{
	Name:   "data",
	Setup6: setup6,
}

const (
	preferredLifeTime = 24 * time.Hour
	validLifeTime     = 24 * time.Hour
	prefixLength      = 80
	macLen            = 6
	// Byte offset where the MAC starts within a 16-byte IPv6 address for a /80 prefix.
	macOffset = 10
)

func parseArgs(args ...string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}
	return args[0], nil
}

func loadConfig(args ...string) (*api.DataConfig, error) {
	path, err := parseArgs(args...)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	log.Debugf("Reading config file %s", path)
	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.DataConfig{}
	if err = yaml.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	_, err := loadConfig(args...)
	if err != nil {
		return nil, err
	}

	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	if req == nil {
		log.Error("Received nil IPv6 request")
		return nil, true
	}

	printer.VerboseRequest(req, log, printer.IPv6)
	defer printer.VerboseResponse(req, resp, log, printer.IPv6)

	if !req.IsRelay() {
		log.Printf("Received non-relay DHCPv6 request. Dropping.")
		return nil, true
	}

	relayMsg := req.(*dhcpv6.RelayMessage)

	mac, err := helper.GetMAC(relayMsg, log)
	if err != nil {
		log.Errorf("Could not determine client MAC: %v", err)
		return nil, true
	}

	if len(mac) != macLen {
		log.Errorf("Unsupported hardware address length %d (expected %d): %s", len(mac), macLen, mac)
		return nil, true
	}

	linkAddr := relayMsg.LinkAddr.To16()
	for i := macOffset; i < net.IPv6len; i++ {
		if linkAddr[i] != 0 {
			log.Errorf("Relay LinkAddr %s has non-zero bits in the last 48 bits, not a valid /80 prefix", relayMsg.LinkAddr)
			return nil, true
		}
	}

	m, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("BUG: could not decapsulate: %v", err)
		return nil, true
	}

	if m.Options.OneIANA() == nil {
		log.Debug("No address requested")
		return resp, false
	}

	ipaddr := buildAddress(relayMsg.LinkAddr, mac)
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
	log.Infof("Client %s, added option IA address %s", macKey, iana.String())

	return resp, false
}

func buildAddress(linkAddr net.IP, mac net.HardwareAddr) net.IP {
	addr := make(net.IP, net.IPv6len)
	copy(addr, linkAddr.To16())

	mask := net.CIDRMask(prefixLength, 128)
	for i := range addr {
		addr[i] &= mask[i]
	}

	copy(addr[macOffset:], mac)
	return addr
}
