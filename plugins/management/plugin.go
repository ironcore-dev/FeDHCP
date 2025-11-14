// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package management

import (
	"net"
	"time"

	"github.com/ironcore-dev/fedhcp/internal/printer"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
	"github.com/mdlayher/netx/eui64"
)

var log = logger.GetLogger("plugins/managament")

var Plugin = plugins.Plugin{
	Name:   "management",
	Setup6: setup6,
}

const (
	preferredLifeTime = 24 * time.Hour
	validLifeTime     = 24 * time.Hour
)

func setup6(_ ...string) (handler.Handler6, error) {
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
		log.Printf("Received non-relay DHCPv6 request, dropping.")
		return nil, true
	}

	relayMsg := req.(*dhcpv6.RelayMessage)

	if len(relayMsg.LinkAddr) != 16 {
		log.Errorf("Received malformed link address of length %d, dropping.", len(relayMsg.LinkAddr))
		return nil, true
	}

	mac, err := getMAC(relayMsg)
	if err != nil {
		log.Errorf("Failed to obtain MAC, dropping: %s", err.Error())
		return nil, true
	}

	if len(mac) != 6 {
		log.Errorf("Received malformed MAC address of length %d, dropping.", len(mac))
		return nil, true
	}

	ipaddr := make(net.IP, len(relayMsg.LinkAddr))
	copy(ipaddr, relayMsg.LinkAddr)

	feEUI64(ipaddr, mac)

	msg, err := req.GetInnerMessage()
	if err != nil {
		log.Errorf("BUG: could not decapsulate: %v", err)
		return nil, true
	}

	if msg.Options.OneIANA() == nil {
		log.Debug("No address requested")
		return resp, false
	}

	iana := &dhcpv6.OptIANA{
		IaId: msg.Options.OneIANA().IaId,
		Options: dhcpv6.IdentityOptions{
			Options: []dhcpv6.Option{
				&dhcpv6.OptIAAddress{
					IPv6Addr:          ipaddr,
					PreferredLifetime: preferredLifeTime,
					ValidLifetime:     validLifeTime,
				},
			},
		},
	}
	resp.AddOption(iana)
	log.Infof("Client %s, added option IA address %s", mac.String(), iana.String())

	return resp, false
}

func getMAC(relayMsg *dhcpv6.RelayMessage) (net.HardwareAddr, error) {
	hwType, mac := relayMsg.Options.ClientLinkLayerAddress()
	if hwType == iana.HWTypeEthernet {
		return mac, nil
	}

	log.Infof("failed to retrieve client link layer address, falling back to EUI64 (%s)", relayMsg.PeerAddr.String())
	_, mac, err := eui64.ParseIP(relayMsg.PeerAddr)
	if err != nil {
		log.Errorf("Could not parse peer address: %s", err)
		return nil, err
	}

	return mac, nil
}

// feEUI64 adjusts the given IP address in-place by overwriting the host part
// using an adaptation of the EUI64 scheme. The two middle bytes are set to 0xfe
// and the first and last three bytes consist of the corresponding first and
// last three bytes of the MAC address. Any pre-existing host bits will be
// overwritten.
//
// Example:
//
//	ipaddr=2001:db8::
//	mac=01:23:45:67:89:ab
//	result=2001:db8::0123:45fe:fe67:89ab
func feEUI64(ipaddr net.IP, mac net.HardwareAddr) {
	// 128 bit == 16 byte, 0-7 are left as-is, 8-15 are modified.
	// 11, 12 get set to 0xfe (EUI64 would use 0xff 0xfe)
	ipaddr[11] = 0xfe
	ipaddr[12] = 0xfe

	copy(ipaddr[8:11], mac[0:3])
	copy(ipaddr[13:16], mac[3:6])
	// TODO: should we flip the 7th bit as EUI64 does it? To me that is just
	// confusing so I won't do it for now.
}
