// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ztp

import (
	"net"
	"os"

	"github.com/mdlayher/netx/eui64"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"gopkg.in/yaml.v3"

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

		It("Setup6 should return a non-nil handler for an empty switches config", func() {
			oldInventory := inventory
			defer func() { inventory = oldInventory }()
			inventory = nil

			config := &api.ZTPConfig{
				Switches: []api.Switch{},
			}
			configData, err := yaml.Marshal(config)
			Expect(err).NotTo(HaveOccurred())

			file, err := os.CreateTemp(GinkgoT().TempDir(), testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = file.Close()
			}()
			Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

			h6, err := setup6(file.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(h6).NotTo(BeNil())
		})

		It("Setup6 should return error if less arguments are provided", func() {
			_, err := setup6()
			Expect(err).To(HaveOccurred())
		})

		It("Setup6 should return error if more arguments are provided", func() {
			_, err := setup6("foo", "bar")
			Expect(err).To(HaveOccurred())
		})

		DescribeTable("Setup6 should return error for a malformed provisioning script address",
			func(scriptAddress string) {
				oldInventory := inventory
				defer func() { inventory = oldInventory }()
				inventory = nil

				_, err := setup6(writeConfig(&api.ZTPConfig{
					Switches: []api.Switch{{
						MacAddress:                inventoryMAC,
						ProvisioningScriptAddress: scriptAddress,
						Name:                      "test-switch",
					}},
				}))
				Expect(err).To(HaveOccurred())
			},
			Entry("unsupported scheme", "ftp://[2001:db8::1]/ztp/provisioning.sh"),
			Entry("missing scheme", "[2001:db8::1]/ztp/provisioning.sh"),
			Entry("missing host", "https:///ztp/provisioning.sh"),
			Entry("missing path", "https://[2001:db8::1]"),
			Entry("unparsable URL", "https://[2001:db8::1/ztp/provisioning.sh"),
		)
	})

	Describe("DHCPv6 Message Handling", func() {
		It("should return provisioning script for known MAC with ZTP option 239 requested", func() {
			req := createRequest(inventoryMAC, true, true)

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

		It("should not return provisioning script for not known MAC with ZTP option 239 requested", func() {
			req := createRequest(nonInventoryMAC, true, true)

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
			req := createRequest(inventoryMAC, true, false)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())
			Expect(stub).NotTo(BeNil())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())

			opt := resp.GetOneOption(optionZTPCode)
			Expect(opt).To(BeNil())
		})

		It("should not return provisioning script, if there are no switches in the inventory", func() {
			oldInventory := inventory
			defer func() { inventory = oldInventory }()
			inventory = nil

			req := createRequest(inventoryMAC, true, true)
			stub := createReply()

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeFalse())
			Expect(resp).To(BeIdenticalTo(stub))
			Expect(resp.GetOneOption(optionZTPCode)).To(BeNil())
		})

		It("should stop and break the plugin chain, if the relayed message cannot be decapsulated", func() {
			// relay message without an encapsulated message
			req := &dhcpv6.RelayMessage{
				MessageType: dhcpv6.MessageTypeRelayForward,
				LinkAddr:    net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"),
				PeerAddr:    net.ParseIP(linkLocalIPV6Prefix),
			}

			resp, stop := handler6(req, createReply())
			Expect(stop).To(BeTrue())
			Expect(resp).To(BeNil())
		})

		It("should stop and break the plugin chain, if the peer address is not an IPv6 address", func() {
			inner, err := dhcpv6.NewMessage()
			Expect(err).NotTo(HaveOccurred())
			inner.MessageType = dhcpv6.MessageTypeRequest
			inner.AddOption(dhcpv6.OptRequestedOption(optionZTPCode))
			req, err := dhcpv6.EncapsulateRelay(inner, dhcpv6.MessageTypeRelayForward,
				net.ParseIP("2001:db8:1111:2222:3333:4444:5555:6666"), net.ParseIP("192.0.2.1"))
			Expect(err).NotTo(HaveOccurred())

			resp, stop := handler6(req, createReply())
			Expect(stop).To(BeTrue())
			Expect(resp).To(BeNil())
		})

		It("should stop and break the plugin chain for non-relayed messages", func() {
			req := createRequest("11:22:33:44:55:66", false, false)

			stub, err := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			Expect(err).NotTo(HaveOccurred())
			Expect(stub).NotTo(BeNil())

			resp, stop := handler6(req, stub)
			Expect(stop).To(BeTrue())
			Expect(resp).To(BeNil())
		})
	})
})

func createRequest(mac string, relayed bool, optZTPRequested bool) dhcpv6.DHCPv6 {
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

func createReply() *dhcpv6.Message {
	stub, err := dhcpv6.NewMessage()
	Expect(err).NotTo(HaveOccurred())
	stub.MessageType = dhcpv6.MessageTypeReply
	return stub
}

func writeConfig(config *api.ZTPConfig) string {
	configData, err := yaml.Marshal(config)
	Expect(err).NotTo(HaveOccurred())

	file, err := os.CreateTemp(GinkgoT().TempDir(), testConfigPath)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

	return file.Name()
}
