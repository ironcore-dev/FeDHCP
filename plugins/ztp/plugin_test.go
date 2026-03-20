// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ztp

import (
	"encoding/binary"
	"net"
	"os"

	"github.com/insomniacslk/dhcp/iana"
	"github.com/mdlayher/netx/eui64"

	"github.com/insomniacslk/dhcp/dhcpv6"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ZTP Plugin", func() {
	Describe("Configuration Loading", func() {
		It("should return an error if the configuration file is missing", func() {
			_, err := loadConfig("nonexistent.yaml")
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the configuration file is invalid", func() {
			invalidConfigPath := "invalid_test_config.yaml"

			file, err := os.CreateTemp(GinkgoT().TempDir(), invalidConfigPath)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = file.Close()
			}()
			Expect(os.WriteFile(file.Name(), []byte("Invalid YAML"), 0644)).To(Succeed())

			_, err = loadConfig(file.Name())
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("DHCPv6 ZTP Message Handling", func() {
		It("should return global provisioning script for known MAC with ZTP option 239 requested", func() {
			req := createZTPRequest(inventoryMAC, true, true)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())
			Expect(stub).NotTo(BeNil())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opt := resp.GetOneOption(optionZTPCode).(*dhcpv6.OptionGeneric)
			Expect(opt).NotTo(BeNil())
			Expect(int(opt.OptionCode)).To(Equal(optionZTPCode))
			Expect(opt.OptionData).To(Equal([]byte(testZtpProvisioningScriptPath)))
		})

		It("should return per-switch override provisioning script when configured", func() {
			req := createZTPRequest(inventoryMACWithOverride, true, true)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())
			Expect(stub).NotTo(BeNil())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opt := resp.GetOneOption(optionZTPCode).(*dhcpv6.OptionGeneric)
			Expect(opt).NotTo(BeNil())
			Expect(int(opt.OptionCode)).To(Equal(optionZTPCode))
			Expect(opt.OptionData).To(Equal([]byte(testZtpOverrideScriptPath)))
		})

		It("should not return provisioning script for unknown MAC with ZTP option 239 requested", func() {
			req := createZTPRequest(nonInventoryMAC, true, true)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())
			Expect(stub).NotTo(BeNil())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opt := resp.GetOneOption(optionZTPCode)
			Expect(opt).To(BeNil())
		})

		It("should not return provisioning script for known MAC with ZTP option 239 not requested", func() {
			req := createZTPRequest(inventoryMAC, true, false)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())
			Expect(stub).NotTo(BeNil())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opt := resp.GetOneOption(optionZTPCode)
			Expect(opt).To(BeNil())
		})

		It("should stop and break the plugin chain for non-relayed messages", func() {
			req := createZTPRequest("11:22:33:44:55:66", false, false)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())
			Expect(stub).NotTo(BeNil())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeTrue())
			Expect(resp).To(BeNil())
		})
	})

	Describe("DHCPv6 ONIE Message Handling", func() {
		It("should return Boot File URL for known MAC with matching vendor", func() {
			req := createONIERequest(inventoryMAC, testONIEVendor)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			bootFileURL := resp.(*dhcpv6.Message).Options.BootFileURL()
			Expect(bootFileURL).To(Equal(testONIEInstallerURL))
		})

		It("should not return Boot File URL for unknown MAC", func() {
			req := createONIERequest(nonInventoryMAC, testONIEVendor)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opts := resp.GetOption(dhcpv6.OptionBootfileURL)
			Expect(opts).To(BeEmpty())
		})

		It("should not return Boot File URL for known MAC with unknown vendor", func() {
			req := createONIERequest(inventoryMAC, testONIEUnknownVendor)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opts := resp.GetOption(dhcpv6.OptionBootfileURL)
			Expect(opts).To(BeEmpty())
		})

		It("should not handle ONIE when Boot File URL is not requested in ORO", func() {
			req := createONIERequestWithoutBootFileURL(inventoryMAC, testONIEVendor)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opts := resp.GetOption(dhcpv6.OptionBootfileURL)
			Expect(opts).To(BeEmpty())
		})

		It("should not handle ONIE when UserClass is missing", func() {
			req := createONIERequestWithoutUserClass(inventoryMAC, testONIEVendor)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opts := resp.GetOption(dhcpv6.OptionBootfileURL)
			Expect(opts).To(BeEmpty())
		})
	})
})

func createZTPRequest(mac string, relayed bool, optZTPRequested bool) dhcpv6.DHCPv6 {
	hwAddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())
	Expect(hwAddr).NotTo(BeNil())

	i := net.ParseIP(linkLocalIPV6Prefix)
	linkLocalIPV6Addr, err := eui64.ParseMAC(i, hwAddr)
	Expect(err).NotTo(HaveOccurred())

	req, err := dhcpv6.NewMessage()
	req.MessageType = dhcpv6.MessageTypeRequest
	Expect(err).NotTo(HaveOccurred())
	Expect(req).NotTo(BeNil())

	if optZTPRequested {
		opt := dhcpv6.OptRequestedOption(optionZTPCode)
		req.AddOption(opt)
	}

	if relayed {
		relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
			net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), linkLocalIPV6Addr)
		Expect(err).NotTo(HaveOccurred())
		Expect(relayedRequest).NotTo(BeNil())

		return relayedRequest
	}

	return req
}

func createONIERequest(mac string, vendorClassData string) dhcpv6.DHCPv6 {
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

	// Add VendorClass with EnterpriseNumber=0 and vendor data
	addONIEVendorClass(req, vendorClassData)

	// Wrap in relay with ClientLinkLayerAddress
	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), linkLocalIPV6Addr)
	Expect(err).NotTo(HaveOccurred())

	// Add Client Link-Layer Address option to relay
	relayedRequest.AddOption(dhcpv6.OptClientLinkLayerAddress(iana.HWTypeEthernet, hwAddr))

	return relayedRequest
}

func createONIERequestWithoutBootFileURL(mac string, vendorClassData string) dhcpv6.DHCPv6 {
	hwAddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())

	i := net.ParseIP(linkLocalIPV6Prefix)
	linkLocalIPV6Addr, err := eui64.ParseMAC(i, hwAddr)
	Expect(err).NotTo(HaveOccurred())

	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest

	// Do NOT request Boot File URL — just request something else
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionDNSRecursiveNameServer))

	addONIEUserClass(req)
	addONIEVendorClass(req, vendorClassData)

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), linkLocalIPV6Addr)
	Expect(err).NotTo(HaveOccurred())
	relayedRequest.AddOption(dhcpv6.OptClientLinkLayerAddress(iana.HWTypeEthernet, hwAddr))

	return relayedRequest
}

func createONIERequestWithoutUserClass(mac string, vendorClassData string) dhcpv6.DHCPv6 {
	hwAddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())

	i := net.ParseIP(linkLocalIPV6Prefix)
	linkLocalIPV6Addr, err := eui64.ParseMAC(i, hwAddr)
	Expect(err).NotTo(HaveOccurred())

	req, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	req.MessageType = dhcpv6.MessageTypeRequest

	// Request Boot File URL but do NOT add UserClass
	req.AddOption(dhcpv6.OptRequestedOption(dhcpv6.OptionBootfileURL))

	addONIEVendorClass(req, vendorClassData)

	relayedRequest, err := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
		net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), linkLocalIPV6Addr)
	Expect(err).NotTo(HaveOccurred())
	relayedRequest.AddOption(dhcpv6.OptClientLinkLayerAddress(iana.HWTypeEthernet, hwAddr))

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

func addONIEVendorClass(req *dhcpv6.Message, vendorData string) {
	data := []byte(vendorData)
	// Wire format: uint32 enterprise number (0) + uint16 length + data
	buf := make([]byte, 4+2+len(data))
	binary.BigEndian.PutUint32(buf[0:4], 0) // EnterpriseNumber = 0
	binary.BigEndian.PutUint16(buf[4:6], uint16(len(data)))
	copy(buf[6:], data)

	optVendorClass := dhcpv6.OptVendorClass{}
	err := optVendorClass.FromBytes(buf)
	Expect(err).NotTo(HaveOccurred())
	req.UpdateOption(&optVendorClass)
}
