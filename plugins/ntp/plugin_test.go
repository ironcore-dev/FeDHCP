// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
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
	It("adds NTP v4 option", func() {
		ntpConfig = &api.NTPConfig{Servers: []net.IP{net.ParseIP("192.0.2.1"), net.ParseIP("1.2.3.4")}}

		req, _ := dhcpv4.New()
		stub, _ := dhcpv4.New()
		resp, stop := handler4(req, stub)
		Expect(stop).NotTo(BeTrue())
		optNTP := resp.GetOneOption(dhcpv4.OptionNTPServers)
		optServerAddresses := parseIPv4ListOption(optNTP)
		Expect(optServerAddresses).To(HaveLen(2))
		Expect(optServerAddresses[0].String()).To(Equal("192.0.2.1"))
		Expect(optServerAddresses[1].String()).To(Equal("1.2.3.4"))
	})

	It("adds NTP v6 option", func() {
		ntpConfig = &api.NTPConfig{
			ServersV6: []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("fe80::1337")},
		}

		req := &dhcpv6.Message{}
		stub := &dhcpv6.Message{}
		resp, stop := handler6(req, stub)
		Expect(stop).NotTo(BeTrue())
		optNTP := resp.GetOneOption(dhcpv6.OptionNTPServer)
		Expect(optNTP).NotTo(BeNil())
		subOpts := optNTP.(*dhcpv6.OptNTPServer).Suboptions
		Expect(subOpts).NotTo(BeNil())
		Expect(len(subOpts)).To(Equal(2))
		subOptNTPServerAddressFirst := subOpts[0].(*dhcpv6.NTPSuboptionSrvAddr)
		Expect(subOptNTPServerAddressFirst.String()).To(Equal("Server Address: 2001:db8::1"))
		subOptNTPServerAddressSecond := subOpts[1].(*dhcpv6.NTPSuboptionSrvAddr)
		Expect(subOptNTPServerAddressSecond.String()).To(Equal("Server Address: fe80::1337"))
	})

	It("skips when no servers", func() {
		ntpConfig = &api.NTPConfig{}

		req, _ := dhcpv4.New()
		stub, _ := dhcpv4.New()
		resp, stop := handler4(req, stub)
		Expect(stop).NotTo(BeTrue())
		Expect(resp.Options.Get(dhcpv4.OptionNTPServers)).To(BeNil())
	})
})

func parseIPv4ListOption(b []byte) []net.IP {
	ips := make([]net.IP, 0, len(b)/4)
	for i := 0; i+4 <= len(b); i += 4 {
		ips = append(ips, net.IPv4(b[i], b[i+1], b[i+2], b[i+3]))
	}
	return ips
}
