// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package stateless

import (
	"fmt"
	"net"
	"os"
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

var log = logger.GetLogger("plugins/stateless")

var Plugin = plugins.Plugin{
	Name:   "stateless",
	Setup6: setup6,
}

const (
	preferredLifeTime = 24 * time.Hour
	validLifeTime     = 24 * time.Hour
	macLen            = 6
)

var (
	macOffset int
	builder   func(net.IP, net.HardwareAddr) net.IP
)

func setup6(args ...string) (handler.Handler6, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("exactly one argument must be passed to the plugin, got %d", len(args))
	}

	log.Debugf("Reading config file %s", args[0])
	configData, err := os.ReadFile(args[0])
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &api.StatelessConfig{}
	err = yaml.Unmarshal(configData, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	switch config.PrefixLength {
	case 64:
		builder = feEUI64
	case 80:
		builder = buildAddressFromMAC
	default:
		return nil, fmt.Errorf("unsupported prefixLength %d, must be 64 or 80", config.PrefixLength)
	}

	macOffset = config.PrefixLength / 8

	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	if req == nil {
		log.Error("Received nil DHCPv6 request")
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
			log.Errorf("Relay LinkAddr %s has non-zero bits in host region (byte %d), not a valid /%d prefix", relayMsg.LinkAddr, i, macOffset*8)
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

	ipaddr := builder(linkAddr, mac)

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
	log.Infof("Client %s, added option IA address %s", mac.String(), iana.String())

	return resp, false
}

// feEUI64 derives an IPv6 address by embedding the MAC into the /64 host part
// using an adaptation of the EUI-64 scheme. The two middle bytes are set to
// 0xfe and the first and last three bytes consist of the corresponding first
// and last three bytes of the MAC address.
//
// Example:
//
//	linkAddr=2001:db8::
//	mac=01:23:45:67:89:ab
//	result=2001:db8::0123:45fe:fe67:89ab
func feEUI64(linkAddr net.IP, mac net.HardwareAddr) net.IP {
	addr := make(net.IP, net.IPv6len)
	copy(addr, linkAddr.To16())

	addr[11] = 0xfe
	addr[12] = 0xfe

	copy(addr[8:11], mac[0:3])
	copy(addr[13:16], mac[3:6])

	return addr
}

// buildAddressFromMAC derives an IPv6 address by copying the raw 6-byte MAC
// into bytes 10-15 of the /80 prefix.
//
// Example:
//
//	linkAddr=2001:db8:1111:2222:3333::
//	mac=aa:bb:cc:dd:ee:ff
//	result=2001:db8:1111:2222:3333:aabb:ccdd:eeff
func buildAddressFromMAC(linkAddr net.IP, mac net.HardwareAddr) net.IP {
	addr := make(net.IP, net.IPv6len)
	copy(addr, linkAddr.To16())

	copy(addr[10:], mac)
	return addr
}
