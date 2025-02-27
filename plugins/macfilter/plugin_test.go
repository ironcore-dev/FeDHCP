// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package macfilter

import (
	"net"
	"os"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"
	"github.com/ironcore-dev/fedhcp/internal/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("Macfilter Plugin", func() {
	var (
		err error
	)

	Describe("Configuration Loading", func() {
		It("should successfully load a valid configuration file", func() {
			config, err := loadConfig(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(config.AllowList[0]).To(Equal(allowListMacPrefix))
			Expect(config.DenyList[0]).To(Equal(denyListMacPrefix))
		})

		It("should return an error if the configuration file is missing", func() {
			_, err := loadConfig("nonexistent.yaml")
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the configuration file is invalid", func() {
			invalidConfigPath := "invalid_test_config.yaml"
			err = os.WriteFile(invalidConfigPath, []byte("Invalid YAML"), 0644)
			Expect(err).NotTo(HaveOccurred())
			_, err = loadConfig(invalidConfigPath)
			Expect(err).To(HaveOccurred())
			err = os.Remove(invalidConfigPath)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("DHCPv6 Message Handling", func() {
		BeforeEach(func() {
			handler, err := setup(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should break the chain if MAC address empty", func() {
			// Create a DUID-LL (Link-Layer Address)
			duidLL := &dhcpv6.DUIDLL{
				HWType:        iana.HWTypeEthernet, // Ethernet (1)
				LinkLayerAddr: nil,
			}
			msg, err := dhcpv6.NewMessage()
			Expect(err).NotTo(HaveOccurred())
			Expect(msg).NotTo(BeNil())

			msg.MessageType = dhcpv6.MessageTypeSolicit
			clientIDOpt := dhcpv6.OptClientID(duidLL)
			msg.AddOption(clientIDOpt)
			opt := msg.GetOneOption(dhcpv6.OptionClientID)
			Expect(opt).NotTo(BeNil())

			_, stop := handler6(msg, nil)
			Expect(stop).To(BeTrue())
		})

		It("should not break the chain if send DUID_LL with allowlist", func() {
			mac, _ := net.ParseMAC(allowListMac)
			// Create a DUID-LL (Link-Layer Address)
			duidLL := &dhcpv6.DUIDLL{
				HWType:        iana.HWTypeEthernet, // Ethernet (1)
				LinkLayerAddr: mac,
			}
			msg, err := dhcpv6.NewMessage()
			Expect(err).NotTo(HaveOccurred())
			Expect(msg).NotTo(BeNil())

			msg.MessageType = dhcpv6.MessageTypeSolicit
			clientIDOpt := dhcpv6.OptClientID(duidLL)
			msg.AddOption(clientIDOpt)
			_, stop := handler6(msg, nil)
			Expect(stop).To(BeFalse())
		})

		It("should break the chain if send request other than DUID_LL, DUID_LLT", func() {
			mac, _ := net.ParseMAC(allowListMac)
			// Create a DUID based on enterprise number
			duidEN := &dhcpv6.DUIDEN{
				EnterpriseNumber:     0,
				EnterpriseIdentifier: mac,
			}
			msg, err := dhcpv6.NewMessage()
			Expect(err).NotTo(HaveOccurred())
			Expect(msg).NotTo(BeNil())
			msg.MessageType = dhcpv6.MessageTypeSolicit
			clientIDOpt := dhcpv6.OptClientID(duidEN)
			msg.AddOption(clientIDOpt)
			_, stop := handler6(msg, nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if OptClientID is nil ", func() {
			msg, err := dhcpv6.NewMessage()
			Expect(err).NotTo(HaveOccurred())
			Expect(msg).NotTo(BeNil())
			msg.MessageType = dhcpv6.MessageTypeSolicit
			_, stop := handler6(msg, nil)
			Expect(stop).To(BeTrue())
		})
	})

	Describe("DHCPv6 Message Handling with only allow listed mac", func() {
		BeforeEach(func() {
			configPath := "tempConfig.yaml"
			config := &api.MACFilterConfig{
				AllowList: []string{allowListMacPrefix},
			}
			configData, err := yaml.Marshal(config)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(configPath, configData, 0644)
			Expect(err).NotTo(HaveOccurred())

			handler, err := setup(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should break the chain if MAC address not matched allow list", func() {
			_, stop := handler6(createMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if MAC address not matched allow list (Relay Message)", func() {
			_, stop := handler6(createRelayMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should not break the chain if MAC address matched allow list", func() {
			_, stop := handler6(createMessage(allowListMac), nil)
			Expect(stop).To(BeFalse())
		})

		It("should not break the chain if MAC address matched allow list (Relay Message)", func() {
			_, stop := handler6(createRelayMessage(allowListMac), nil)
			Expect(stop).To(BeFalse())
		})
	})

	Describe("DHCPv6 Message Handling with only deny listed mac", func() {
		BeforeEach(func() {
			configPath := "tempConfig.yaml"
			config := &api.MACFilterConfig{
				DenyList: []string{denyListMacPrefix},
			}
			configData, err := yaml.Marshal(config)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(configPath, configData, 0644)
			Expect(err).NotTo(HaveOccurred())

			handler, err := setup(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should break the chain if MAC address matched deny list with no allow list defined", func() {
			_, stop := handler6(createMessage(denyListMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if MAC address matched deny list with no allow list defined (Relay Message)", func() {
			_, stop := handler6(createRelayMessage(denyListMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should not break the chain if MAC address not matched deny list with no allow list defined", func() {
			_, stop := handler6(createMessage(unmatchedMac), nil)
			Expect(stop).To(BeFalse())
		})

		It("should not break the chain if MAC address not matched deny list with no allow list defined (Relay Message)", func() {
			_, stop := handler6(createRelayMessage(unmatchedMac), nil)
			Expect(stop).To(BeFalse())
		})
	})

	Describe("DHCPv6 Message Handling with both allowed and deny listed mac", func() {
		BeforeEach(func() {
			configPath := "tempConfig.yaml"
			config := &api.MACFilterConfig{
				AllowList: []string{"11:22", "33"},
				DenyList:  []string{"11", "33:44"},
			}
			configData, err := yaml.Marshal(config)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(configPath, configData, 0644)
			Expect(err).NotTo(HaveOccurred())

			handler, err := setup(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should break the chain if MAC address matched both lists and deny list includes the allow list", func() {
			_, stop := handler6(createMessage("11:22:33:44:55:66"), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if MAC address matched both lists and deny list includes the allow list (Relay Message)", func() {
			_, stop := handler6(createRelayMessage("11:22:33:44:55:66"), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if MAC address matched both lists", func() {
			_, stop := handler6(createMessage("33:44:33:33:33:33"), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if MAC address matched deny lists (Relay Message)", func() {
			_, stop := handler6(createRelayMessage("33:44:33:33:33:33"), nil)
			Expect(stop).To(BeTrue())
		})

		It("should not break the chain if MAC address matched the allow list only", func() {
			_, stop := handler6(createMessage("33:33:33:33:33:33"), nil)
			Expect(stop).To(BeFalse())
		})

		It("should not break the chain if MAC address matched the allow list only (Relay Message)", func() {
			_, stop := handler6(createRelayMessage("33:33:33:33:33:33"), nil)
			Expect(stop).To(BeFalse())
		})

		It("should break the chain if MAC address not matched any list", func() {
			_, stop := handler6(createMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if MAC address not matched any list (Relay Message)", func() {
			_, stop := handler6(createRelayMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})
	})
})

func createMessage(mac string) dhcpv6.DHCPv6 {
	hwaddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())
	Expect(hwaddr).NotTo(BeNil())
	req, err := dhcpv6.NewSolicit(hwaddr)
	Expect(err).NotTo(HaveOccurred())
	Expect(req).NotTo(BeNil())
	return req
}

func createRelayMessage(mac string) dhcpv6.DHCPv6 {
	hwaddr, err := net.ParseMAC(mac)
	Expect(err).NotTo(HaveOccurred())
	Expect(hwaddr).NotTo(BeNil())
	req, err := dhcpv6.NewSolicit(hwaddr)
	Expect(err).NotTo(HaveOccurred())
	Expect(req).NotTo(BeNil())
	req.MessageType = dhcpv6.MessageTypeRelayForward
	return req
}
