// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"fmt"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	"net"
	"time"

	"github.com/coredhcp/coredhcp/handler"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"

	"github.com/mdlayher/netx/eui64"
)

var log = logger.GetLogger("plugins/oob")

var Plugin = plugins.Plugin{
	Name:   "oob",
	Setup4: setup4,
	Setup6: setup6,
}

var (
	k8sClient *K8sClient
)

const (
	UNKNOWN_IP = "0.0.0.0"
)

func parseArgs(args ...string) (string, string, error) {
	if len(args) < 2 {
		return "", "", fmt.Errorf("at least two arguments must be passed to ipam plugin, a namespace and a OOB subnet label, got %d", len(args))
	}

	namespace := args[0]
	oobLabel := args[1]
	return namespace, oobLabel, nil
}

func setup6(args ...string) (handler.Handler6, error) {
	namespace, oobLabel, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}

	k8sClient, err = NewK8sClient(namespace, oobLabel)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	log.Print("Loaded oob plugin for DHCPv6.")
	return handler6, nil
}

func handler6(req, resp dhcpv6.DHCPv6) (dhcpv6.DHCPv6, bool) {
	log.Debugf("received DHCPv6 packet: %s", req.Summary())

	if !req.IsRelay() {
		log.Printf("Received non-relay DHCPv6 request. Dropping.")
		return nil, true
	}

	relayMsg := req.(*dhcpv6.RelayMessage)

	// Retrieve IPv6 prefix and MAC address from IPv6 address
	_, mac, err := eui64.ParseIP(relayMsg.PeerAddr)
	if err != nil {
		log.Errorf("Could not parse peer address: %s", err)
		return nil, true
	}

	ipaddr := make(net.IP, len(relayMsg.LinkAddr))
	copy(ipaddr, relayMsg.LinkAddr)

	log.Infof("Requested IP address from relay %s for mac %s", ipaddr.String(), mac.String())
	leaseIP, err := k8sClient.getIp(ipaddr, mac, false, ipamv1alpha1.CIPv6SubnetType)
	if err != nil {
		log.Errorf("Could not get IPAM IP: %s", err)
		return nil, true
	}

	var m *dhcpv6.Message
	m, err = req.GetInnerMessage()
	if err != nil {
		log.Errorf("BUG: could not decapsulate: %v", err)
		return nil, true
	}

	if m.Options.OneIANA() == nil {
		log.Debug("No address requested")
		return resp, false
	}

	resp.AddOption(&dhcpv6.OptIANA{
		IaId: m.Options.OneIANA().IaId,
		Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
			&dhcpv6.OptIAAddress{
				IPv6Addr:          leaseIP,
				PreferredLifetime: 24 * time.Hour,
				ValidLifetime:     24 * time.Hour,
			},
		}},
	})

	log.Debugf("Sent DHCPv6 response: %s", resp.Summary())

	return resp, false
}

func setup4(args ...string) (handler.Handler4, error) {
	namespace, oobLabel, err := parseArgs(args...)
	if err != nil {
		return nil, err
	}

	k8sClient, err = NewK8sClient(namespace, oobLabel)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	log.Print("Loaded oob plugin for DHCPv4.")
	return handler4, nil
}

func handler4(req, resp *dhcpv4.DHCPv4) (*dhcpv4.DHCPv4, bool) {
	mac := req.ClientHWAddr

	log.Debugf("received DHCPv4 packet: %s", req.Summary())
	log.Tracef("Message type: %s", req.MessageType().String())

	var ipaddr net.IP
	var exactIP bool

	serverIP := resp.ServerIPAddr
	clientIP := req.ClientIPAddr
	requestedIP := dhcpv4.GetIP(dhcpv4.OptionRequestedIPAddress, req.Options)
	if clientIP != nil {
		// ack requested address
		exactIP = true
		ipaddr = clientIP
		log.Debugf("IP client: %v", ipaddr)
	} else if requestedIP != nil {
		// ack requested address
		exactIP = true
		ipaddr = requestedIP
		log.Debugf("IP client: %v", ipaddr)
	} else if serverIP != nil {
		// no client information, use server address for subnet detection
		exactIP = false
		ipaddr = serverIP
		log.Debugf("IP server: %v", ipaddr)
	} else {
		ipaddr = net.ParseIP(UNKNOWN_IP)
		exactIP = false
	}

	log.Debugf("IP: %v", ipaddr)
	leaseIP, err := k8sClient.getIp(ipaddr, mac, exactIP, ipamv1alpha1.CIPv4SubnetType)
	if err != nil {
		log.Errorf("Could not get IPAM IP: %s", err)
		return nil, true
	}

	resp.YourIPAddr = leaseIP

	log.Debugf("Sent DHCPv4 response: %s", resp.Summary())

	return resp, false
}
