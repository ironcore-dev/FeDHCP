// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package onie

import (
	"encoding/binary"
	"net"

	"github.com/mdlayher/netx/eui64"

	"github.com/insomniacslk/dhcp/dhcpv6"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ONIE Plugin", func() {
	Describe("DHCPv6 ONIE Message Handling", func() {
		It("should return Boot File URL for a valid ONIE request", func() {
			req := createONIERequest(testMAC)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			bootFileURL := resp.(*dhcpv6.Message).Options.BootFileURL()
			Expect(bootFileURL).To(Equal(testONIEImagesAddress))
		})

		It("should not return Boot File URL when Boot File URL is not in ORO", func() {
			req := createONIERequestWithoutBootFileURL(testMAC)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opts := resp.GetOption(dhcpv6.OptionBootfileURL)
			Expect(opts).To(BeEmpty())
		})

		It("should not return Boot File URL when UserClass is missing", func() {
			req := createONIERequestWithoutUserClass(testMAC)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opts := resp.GetOption(dhcpv6.OptionBootfileURL)
			Expect(opts).To(BeEmpty())
		})

		It("should drop non-relay DHCPv6 requests", func() {
			req, err := dhcpv6.NewMessage()
			Expect(err).NotTo(HaveOccurred())
			req.MessageType = dhcpv6.MessageTypeRequest
			req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeTrue())
			Expect(resp).To(BeNil())
		})
	})
})

func createONIERequest(mac string) dhcpv6.DHCPv6 {
	hwAddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())

	i := net.ParseIP(linkLocalIPV6Prefix)
	linkLocalIPV6Addr, err := eui64.ParseMAC(i, hwAddr)
	Expect(err).NotTo(HaveOccurred())

	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest

	// Request Boot File URL (option 59)
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))

	// Add UserClass with "onie_dhcp_user_class"
	addONIEUserClass(req)

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), linkLocalIPV6Addr)
	Expect(err).NotTo(HaveOccurred())

	return relayedRequest
}

func createONIERequestWithoutBootFileURL(mac string) dhcpv6.DHCPv6 {
	hwAddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())

	i := net.ParseIP(linkLocalIPV6Prefix)
	linkLocalIPV6Addr, err := eui64.ParseMAC(i, hwAddr)
	Expect(err).NotTo(HaveOccurred())

	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest

	// Do NOT request Boot File URL
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionDNSRecursiveNameServer))

	addONIEUserClass(req)

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), linkLocalIPV6Addr)
	Expect(err).NotTo(HaveOccurred())

	return relayedRequest
}

func createONIERequestWithoutUserClass(mac string) dhcpv6.DHCPv6 {
	hwAddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())

	i := net.ParseIP(linkLocalIPV6Prefix)
	linkLocalIPV6Addr, err := eui64.ParseMAC(i, hwAddr)
	Expect(err).NotTo(HaveOccurred())

	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest

	// Request Boot File URL but no UserClass
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), linkLocalIPV6Addr)
	Expect(err).NotTo(HaveOccurred())

	return relayedRequest
}

func addONIEUserClass(req *dhcpv6.Message) {
	userClassData := []byte(onieUserClass)
	buf := make([]byte, 2+len(userClassData))
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(userClassData)))
	copy(buf[2:], userClassData)

	optUserClass := dhcpv6.OptUserClass{}
	err := optUserClass.FromBytes(buf)
	Expect(err).NotTo(HaveOccurred())
	req.UpdateOption(&optUserClass)
}
