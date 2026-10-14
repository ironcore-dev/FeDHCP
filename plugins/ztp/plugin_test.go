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

const testSwitchName = "test-switch"

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

	Describe("Switch Inventory", func() {
		BeforeEach(func() {
			// the inventory is shared with the other specs
			oldInventory := inventory
			DeferCleanup(func() { inventory = oldInventory })
		})

		DescribeTable("should return provisioning script for a known MAC in any notation",
			func(configuredMAC string) {
				_, err := setup6(writeConfig(&api.ZTPConfig{
					Switches: []api.Switch{{
						MacAddress:                configuredMAC,
						ProvisioningScriptAddress: testZtpProvisioningScriptPath,
						Name:                      testSwitchName,
					}},
				}))
				Expect(err).NotTo(HaveOccurred())

				resp, stop := handler6(createRequest("aa:bb:cc:dd:ee:ff", true, true), createReply())
				Expect(stop).To(BeFalse())

				opt := resp.GetOneOption(optionZTPCode)
				Expect(opt).NotTo(BeNil())
				Expect(opt.(*dhcpv6.OptionGeneric).OptionData).To(Equal([]byte(testZtpProvisioningScriptPath)))
			},
			Entry("lower case", "aa:bb:cc:dd:ee:ff"),
			Entry("upper case", "AA:BB:CC:DD:EE:FF"),
			Entry("hyphen separated", "aa-bb-cc-dd-ee-ff"),
		)

		It("Setup6 should return error for an invalid switch MAC address", func() {
			_, err := setup6(writeConfig(&api.ZTPConfig{
				Switches: []api.Switch{{
					MacAddress:                "not-a-mac",
					ProvisioningScriptAddress: testZtpProvisioningScriptPath,
					Name:                      testSwitchName,
				}},
			}))
			Expect(err).To(HaveOccurred())
		})

		It("Setup6 should replace the inventory of a previous setup", func() {
			configFile := writeConfig(&api.ZTPConfig{
				Switches: []api.Switch{{
					MacAddress:                inventoryMAC,
					ProvisioningScriptAddress: testZtpProvisioningScriptPath,
					Name:                      testSwitchName,
				}},
			})

			for range 2 {
				_, err := setup6(configFile)
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(inventory).To(HaveLen(1))
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
