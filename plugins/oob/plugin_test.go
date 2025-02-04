// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/mdlayher/netx/eui64"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("OOB Plugin", func() {
	var (
		testConfigPath string
		err            error
	)

	BeforeEach(func() {
		// Setup temporary test config file
		testConfigPath = "oob_config.yaml"
		config := &api.OOBConfig{
			Namespace:   "oob-ns",
			SubnetLabel: "subnet=dhcp",
		}
		configData, err := yaml.Marshal(config)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(testConfigPath, configData, 0644)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		// Cleanup temporary config file
		err = os.Remove(testConfigPath)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Configuration Loading", func() {
		It("should successfully load a valid configuration file", func() {
			config, err := loadConfig(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(config.Namespace).To(Equal("oob-ns"))
			Expect(config.SubnetLabel).To(Equal("subnet=dhcp"))
		})

		It("should return an error if the configuration file is missing", func() {
			_, err := loadConfig("nonexistent.yaml")
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if the configuration file is invalid", func() {
			err = os.WriteFile(testConfigPath, []byte("Invalid YAML"), 0644)
			Expect(err).NotTo(HaveOccurred())
			_, err = loadConfig(testConfigPath)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Plugin Setup6", func() {
		It("should successfully initialize the plugin with a valid config", func() {
			handler, err := setup6(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should return an error for invalid subnetLabel in the config", func() {
			invalidConfig := &api.OOBConfig{
				Namespace:   "oob-ns",
				SubnetLabel: "subnet-dhcp",
			}
			invalidConfigData, err := yaml.Marshal(invalidConfig)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(testConfigPath, invalidConfigData, 0644)
			Expect(err).NotTo(HaveOccurred())

			_, err = setup6(testConfigPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be 'key=value'"))
		})
	})

	Describe("Plugin Setup4", func() {
		It("should successfully initialize the plugin with a valid config", func() {
			handler, err := setup4(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should return an error for invalid subnetLabel in the config", func() {
			invalidConfig := &api.OOBConfig{
				Namespace:   "oob-ns",
				SubnetLabel: "subnet-dhcp",
			}
			invalidConfigData, err := yaml.Marshal(invalidConfig)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(testConfigPath, invalidConfigData, 0644)
			Expect(err).NotTo(HaveOccurred())

			_, err = setup4(testConfigPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be 'key=value'"))
		})
	})

	Describe("Plugin handler6", func() {
		It("should successfully handle request", func() {
			// handler, err1 := setup6(testConfigPath)
			// Expect(err1).NotTo(HaveOccurred())
			// Expect(handler).NotTo(BeNil())

			// _, err2 := NewK8sClient("oob-ns", "subnet")
			// Expect(err2).NotTo(HaveOccurred())

			mac, _ := net.ParseMAC(unknownMachineMACAddress)
			ip := net.ParseIP(linkLocalIPV6Prefix)
			linkLocalIPV6Addr, _ := eui64.ParseMAC(ip, mac)

			req, _ := dhcpv6.NewMessage()
			req.MessageType = dhcpv6.MessageTypeRequest
			relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, linkLocalIPV6Addr)

			// stub, _ := dhcpv6.NewMessage()
			// stub.MessageType = dhcpv6.MessageTypeReply
			resp, err := handler6(relayedRequest, nil)
			fmt.Printf("error %v", err)
			Expect(err).To(BeNil())
			Expect(resp).NotTo(BeNil())
			respm, stop := resp.GetInnerMessage()
			Expect(stop).To(BeFalse())

			// stub.AddOption(&dhcpv6.OptIANA{
			// 	IaId: stub.Options.OneIANA().IaId,
			// 	Options: dhcpv6.IdentityOptions{Options: []dhcpv6.Option{
			// 		&dhcpv6.OptIAAddress{
			// 			IPv6Addr:          linkLocalIPV6Addr,
			// 			PreferredLifetime: 24 * time.Hour,
			// 			ValidLifetime:     24 * time.Hour,
			// 		},
			// 	}},
			// })

			Expect(respm.MessageType).To(Equal(dhcpv6.MessageTypeRequest))
			Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).IPv6Addr.String()).To(Equal(linkLocalIPV6Prefix))
			Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime).To(Equal(24 * time.Hour))
			Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime).To(Equal(24 * time.Hour))

			//Eventually(Get(stub)).Should(Satisfy(apierrors.IsNotFound))
		})

		It("should return an error for invalid subnetLabel in the config", func() {
			invalidConfig := &api.OOBConfig{
				Namespace:   "oob-ns",
				SubnetLabel: "subnet-dhcp",
			}
			invalidConfigData, err := yaml.Marshal(invalidConfig)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(testConfigPath, invalidConfigData, 0644)
			Expect(err).NotTo(HaveOccurred())

			_, err = setup4(testConfigPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be 'key=value'"))
		})
	})

})
