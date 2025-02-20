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
			Expect(config.WhiteList[0]).To(Equal(whiteListMacPrefix))
			Expect(config.BlackList[0]).To(Equal(blackListMacPrefix))
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

		It("should break the chain if white list MAC address not matched", func() {
			_, stop := handleDHCPv6(createMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if white list MAC address not matched (Relay Message)", func() {
			_, stop := handleDHCPv6(createRelayMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if white list MAC address not matched, take precedence to white listed mac", func() {
			_, stop := handleDHCPv6(createMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if white list MAC address not matched, take precedence to white listed mac (Relay Message)", func() {
			_, stop := handleDHCPv6(createRelayMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should not break the chain if white list MAC address matched", func() {
			_, stop := handleDHCPv6(createMessage(whiteListMac), nil)
			Expect(stop).To(BeFalse())
		})

		It("should not break the chain if white list MAC address matched (Relay Message)", func() {
			_, stop := handleDHCPv6(createRelayMessage(whiteListMac), nil)
			Expect(stop).To(BeFalse())
		})

		It("should break the chain if black list MAC address matched", func() {
			_, stop := handleDHCPv6(createMessage(blackListMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if black list MAC address matched (Relay Message)", func() {
			_, stop := handleDHCPv6(createRelayMessage(blackListMac), nil)
			Expect(stop).To(BeTrue())
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

			_, stop := handleDHCPv6(msg, nil)
			Expect(stop).To(BeTrue())
		})

		It("should not break the chain if send DUID_LL with whitelist", func() {
			mac, _ := net.ParseMAC(whiteListMac)
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
			_, stop := handleDHCPv6(msg, nil)
			Expect(stop).To(BeFalse())
		})

		It("should break the chain if send request other than DUID_LL, DUID_LLT", func() {
			mac, _ := net.ParseMAC(whiteListMac)
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
			_, stop := handleDHCPv6(msg, nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if OptClientID is nil ", func() {
			msg, err := dhcpv6.NewMessage()
			Expect(err).NotTo(HaveOccurred())
			Expect(msg).NotTo(BeNil())
			msg.MessageType = dhcpv6.MessageTypeSolicit
			_, stop := handleDHCPv6(msg, nil)
			Expect(stop).To(BeTrue())
		})
	})

	Describe("DHCPv6 Message Handling with only white listed mac", func() {
		BeforeEach(func() {
			config := &api.MACFilterConfig{
				WhiteList: []string{whiteListMacPrefix},
			}
			configData, err := yaml.Marshal(config)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(testConfigPath, configData, 0644)
			Expect(err).NotTo(HaveOccurred())

			handler, err := setup(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should break the chain if white list MAC address not matched (Relay Message)", func() {
			_, stop := handleDHCPv6(createRelayMessage(unmatchedMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should not break the chain if white list MAC address matched (Relay Message)", func() {
			_, stop := handleDHCPv6(createRelayMessage(whiteListMac), nil)
			Expect(stop).To(BeFalse())
		})
	})

	Describe("DHCPv6 Message Handling with only black listed mac", func() {
		BeforeEach(func() {
			config := &api.MACFilterConfig{
				BlackList: []string{blackListMacPrefix},
			}
			configData, err := yaml.Marshal(config)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(testConfigPath, configData, 0644)
			Expect(err).NotTo(HaveOccurred())

			handler, err := setup(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should break the chain if black list MAC address matched", func() {
			_, stop := handleDHCPv6(createMessage(blackListMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should break the chain if black list MAC address matched (Relay Message)", func() {
			_, stop := handleDHCPv6(createRelayMessage(blackListMac), nil)
			Expect(stop).To(BeTrue())
		})

		It("should not break the chain if black list MAC address not matched in case white listed mac not defined", func() {
			_, stop := handleDHCPv6(createMessage(unmatchedMac), nil)
			Expect(stop).To(BeFalse())
		})

		It("should not break the chain if black list MAC address not matched in case white listed mac not defined (Relay Message)", func() {
			_, stop := handleDHCPv6(createRelayMessage(unmatchedMac), nil)
			Expect(stop).To(BeFalse())
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
