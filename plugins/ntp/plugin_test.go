// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ntp

import (
	"net"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NTP Plugin", func() {
	SetupTest()

	It("Setup6 should return error if less arguments are provided", func() {
		_, err := setup6()
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if more arguments are provided", func() {
		_, err := setup6("foo", "bar")
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if config file does not exist", func() {
		_, err := setup6("does-not-exist.yaml")
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return error if config file is invalid", func() {
		_, err := setup6(writeConfigData([]byte("Invalid YAML")))
		Expect(err).To(HaveOccurred())
	})

	It("Setup4 should return error if less arguments are provided", func() {
		_, err := setup4()
		Expect(err).To(HaveOccurred())
	})

	It("Setup4 should return error if more arguments are provided", func() {
		_, err := setup4("foo", "bar")
		Expect(err).To(HaveOccurred())
	})

	It("Setup4 should return error if config file does not exist", func() {
		_, err := setup4("does-not-exist.yaml")
		Expect(err).To(HaveOccurred())
	})

	It("Setup4 should return error if config file is invalid", func() {
		_, err := setup4(writeConfigData([]byte("Invalid YAML")))
		Expect(err).To(HaveOccurred())
	})

	It("Setup6 should return a non-nil handler for an empty config", func() {
		h6, err := setup6(writeConfig(api.NTPConfig{}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h6).NotTo(BeNil())
		Expect(ntpConfig.ServersV6).To(BeEmpty())
	})

	It("Setup4 should return a non-nil handler for an empty config", func() {
		h4, err := setup4(writeConfig(api.NTPConfig{}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h4).NotTo(BeNil())
		Expect(ntpConfig.Servers).To(BeEmpty())
	})

	It("Should return a valid config for a valid config file", func() {
		config, err := loadConfig(writeConfigData([]byte(
			"servers:\n" +
				"  - " + ntpServerIPV4Address1 + "\n" +
				"  - " + ntpServerIPV4Address2 + "\n" +
				"servers_v6:\n" +
				"  - " + ntpServerIPV6Address1 + "\n" +
				"  - " + ntpServerIPV6Address2 + "\n")))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Servers).To(Equal(ntpServersV4))
		Expect(config.ServersV6).To(Equal(ntpServersV6))
	})

	It("Should return an error for a config file with an invalid server address", func() {
		_, err := loadConfig(writeConfigData([]byte("servers:\n  - not-an-ip\n")))
		Expect(err).To(HaveOccurred())
	})

	It("Should add the NTP servers option to an IPv4 DHCP reply, if requested", func() {
		req := newRequest4(dhcpv4.WithRequestedOptions(dhcpv4.OptionNTPServers))
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))
		Expect(resp.NTPServers()).To(HaveExactElements(
			net.ParseIP(ntpServerIPV4Address1).To4(),
			net.ParseIP(ntpServerIPV4Address2).To4(),
		))
	})

	It("Should add the NTP servers option to an IPv4 DHCP reply, if no options are explicitly requested", func() {
		// RFC2131 3.5: all available parameters are sent if there is no parameter request list
		req := newRequest4()
		Expect(req.ParameterRequestList()).To(BeNil())
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp.NTPServers()).To(HaveLen(2))
	})

	It("Should replace an existing NTP servers option in an IPv4 DHCP reply", func() {
		req := newRequest4(dhcpv4.WithRequestedOptions(dhcpv4.OptionNTPServers))
		stub, _ := dhcpv4.NewReplyFromRequest(req,
			dhcpv4.WithOption(dhcpv4.OptNTPServers(net.ParseIP("10.0.0.1"))))

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp.NTPServers()).To(HaveExactElements(
			net.ParseIP(ntpServerIPV4Address1).To4(),
			net.ParseIP(ntpServerIPV4Address2).To4(),
		))
	})

	It("Should not add the NTP servers option to an IPv4 DHCP reply, if not requested", func() {
		req := newRequest4(dhcpv4.WithRequestedOptions(dhcpv4.OptionDomainNameServer))
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))
		Expect(resp.Options.Has(dhcpv4.OptionNTPServers)).To(BeFalse())
	})

	It("Should not add the NTP servers option to an IPv4 DHCP reply, if no IPv4 servers are configured", func() {
		h4, err := setup4(writeConfig(api.NTPConfig{ServersV6: ntpServersV6}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h4).NotTo(BeNil())

		req := newRequest4(dhcpv4.WithRequestedOptions(dhcpv4.OptionNTPServers))
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		resp, breakChain := handler4(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp.Options.Has(dhcpv4.OptionNTPServers)).To(BeFalse())
	})

	It("Should add the NTP server option to an IPv6 DHCP reply for a relayed request, if requested", func() {
		req := newRequest6(dhcpv6.OptionNTPServer)
		relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
			net.ParseIP(relayIPV6Address), net.ParseIP(linkLocalIPV6Address))
		Expect(err).NotTo(HaveOccurred())
		stub := newReply6()

		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))
		Expect(ntpServerAddresses(resp)).To(HaveExactElements(ntpServerIPV6Address1, ntpServerIPV6Address2))
	})

	It("Should add the NTP server option to an IPv6 DHCP reply for a direct request, if requested", func() {
		req := newRequest6(dhcpv6.OptionNTPServer)
		stub := newReply6()

		resp, breakChain := handler6(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))
		Expect(ntpServerAddresses(resp)).To(HaveExactElements(ntpServerIPV6Address1, ntpServerIPV6Address2))
	})

	It("Should not add the NTP server option to an IPv6 DHCP reply, if not requested", func() {
		req := newRequest6(dhcpv6.OptionDNSRecursiveNameServer)
		stub := newReply6()

		resp, breakChain := handler6(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))
		Expect(resp.GetOneOption(dhcpv6.OptionNTPServer)).To(BeNil())
	})

	It("Should not add the NTP server option to an IPv6 DHCP reply, if no options are requested", func() {
		req := newRequest6()
		stub := newReply6()

		resp, breakChain := handler6(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp.GetOneOption(dhcpv6.OptionNTPServer)).To(BeNil())
	})

	It("Should not add the NTP server option to an IPv6 DHCP reply, if no IPv6 servers are configured", func() {
		h6, err := setup6(writeConfig(api.NTPConfig{Servers: ntpServersV4}))
		Expect(err).NotTo(HaveOccurred())
		Expect(h6).NotTo(BeNil())

		req := newRequest6(dhcpv6.OptionNTPServer)
		stub := newReply6()

		resp, breakChain := handler6(req, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp.GetOneOption(dhcpv6.OptionNTPServer)).To(BeNil())
	})

	It("Should return the unchanged IPv6 DHCP reply and not break plugin chain, if the relayed request cannot be decapsulated", func() {
		// relay message without an encapsulated message
		relayedRequest := &dhcpv6.RelayMessage{
			MessageType: dhcpv6.MessageTypeRelayForward,
			LinkAddr:    net.ParseIP(relayIPV6Address),
			PeerAddr:    net.ParseIP(linkLocalIPV6Address),
		}
		stub := newReply6()

		resp, breakChain := handler6(relayedRequest, stub)
		Expect(breakChain).To(BeFalse())
		Expect(resp).To(BeIdenticalTo(stub))
		Expect(resp.GetOneOption(dhcpv6.OptionNTPServer)).To(BeNil())
	})
})

// newRequest4 returns a DHCPv4 discover, which has no parameter request list unless set by a modifier.
func newRequest4(modifiers ...dhcpv4.Modifier) *dhcpv4.DHCPv4 {
	mac, err := net.ParseMAC(clientMACAddress)
	Expect(err).NotTo(HaveOccurred())

	modifiers = append([]dhcpv4.Modifier{
		dhcpv4.WithHwAddr(mac),
		dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover),
	}, modifiers...)
	req, err := dhcpv4.New(modifiers...)
	Expect(err).NotTo(HaveOccurred())
	return req
}

func newRequest6(requestedOptions ...dhcpv6.OptionCode) *dhcpv6.Message {
	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest
	if len(requestedOptions) > 0 {
		req.AddOption(dhcpv6.OptRequestedOption(requestedOptions...))
	}
	return req
}

func newReply6() *dhcpv6.Message {
	stub, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	stub.MessageType = dhcpv6.MessageTypeReply
	return stub
}

// ntpServerAddresses returns the server address suboptions of the NTP server option of a DHCPv6 message.
func ntpServerAddresses(msg dhcpv6.DHCPv6) []string {
	opt := msg.GetOneOption(dhcpv6.OptionNTPServer)
	Expect(opt).NotTo(BeNil())
	Expect(opt).To(BeAssignableToTypeOf(&dhcpv6.OptNTPServer{}))

	subOpts := opt.(*dhcpv6.OptNTPServer).Suboptions
	addresses := make([]string, 0, len(subOpts))
	for _, subOpt := range subOpts {
		Expect(subOpt).To(BeAssignableToTypeOf(&dhcpv6.NTPSuboptionSrvAddr{}))
		addresses = append(addresses, net.IP(*subOpt.(*dhcpv6.NTPSuboptionSrvAddr)).String())
	}
	return addresses
}
