// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"net"
	"os"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	"github.com/mdlayher/netx/eui64"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("OOB Plugin", func() {
	var (
		err error
	)

	Describe("Configuration Loading", func() {
		It("should successfully load a valid configuration file", func() {
			config, err := loadConfig(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(config.Namespace).To(Equal(ns.Name))
			Expect(config.SubnetLabel).To(Equal("subnet=foo"))
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
		It("should return an error for invalid subnetLabel in the config", func() {
			invalidConfig := &api.OOBConfig{
				Namespace:   ns.Name,
				SubnetLabel: "subnet-foo",
			}
			invalidConfigData, err := yaml.Marshal(invalidConfig)
			Expect(err).NotTo(HaveOccurred())
			file, err := os.CreateTemp(GinkgoT().TempDir(), "invalidConfig.yaml")
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = file.Close()
			}()
			Expect(os.WriteFile(file.Name(), invalidConfigData, 0644)).To(Succeed())

			_, err = setup6(file.Name())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be 'key=value'"))
		})

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

	})

	Describe("Plugin handler6", func() {
		It("Should break plugin chain, if getting an IPv6 DHCP request directly (no relay)", func() {
			req, _ := dhcpv6.NewMessage()
			req.MessageType = dhcpv6.MessageTypeRequest

			stub, _ := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			resp, breakChain := handler6(req, stub)

			Eventually(resp).Should(BeNil())
			Eventually(breakChain).Should(BeTrue())
		})

		It("should successfully handle request", func() {
			req, _ := dhcpv6.NewMessage()
			req.MessageType = dhcpv6.MessageTypeRequest
			relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, linkLocalIPV6Addr, linkLocalIPV6Addr)

			res, _ := dhcpv6.NewMessage()
			resp, stop := handler6(relayedRequest, res)
			Expect(stop).To(BeFalse())
			Expect(resp).NotTo(BeNil())
		})
	})

	Describe("K8s Client tests", func() {
		It("should successfully match the subnet", func() {
			subnets := k8sClient.getOOBNetworks(ipamv1alpha1.CIPv6SubnetType)
			Expect(subnets).NotTo(BeNil())
			Expect(subnets).To(HaveLen(1))
		})

		It("should match the subnet", func() {
			subnet, err := k8sClient.getMatchingSubnet("foo-v6", linkLocalIPV6Addr)
			Expect(err).NotTo(HaveOccurred())
			Expect(subnet).NotTo(BeNil())
		})

		It("should return (nil, nil) and not match the subnet if random subnet passed", func() {
			subnet, err := k8sClient.getMatchingSubnet("randomfoo", linkLocalIPV6Addr)
			Expect(err).ToNot(HaveOccurred())
			Expect(subnet).To(BeNil())
		})

		It("should not match the subnet", func() {
			m, err := net.ParseMAC(unknownMachineMACAddress)
			Expect(err).NotTo(HaveOccurred())
			i := net.ParseIP("fe90::")
			unknownIPV6Addr, err := eui64.ParseMAC(i, m)
			Expect(err).NotTo(HaveOccurred())

			subnet, err := k8sClient.getMatchingSubnet("foo", unknownIPV6Addr)
			Expect(err).ToNot(HaveOccurred())
			Expect(subnet).To(BeNil())
		})

		It("return true checks the ip in CIDR", func() {
			checkIP := checkIPInCIDR(linkLocalIPV6Addr, "fe80::/64")
			Expect(checkIP).To(BeTrue())
		})

		It("return false, if invalid CIDR", func() {
			checkIP := checkIPInCIDR(linkLocalIPV6Addr, "fe80::")
			Expect(checkIP).To(BeFalse())
		})
	})

	Describe("Plugin Setup4", func() {
		It("should return an error for invalid subnetLabel in the config", func() {
			invalidConfig := &api.OOBConfig{
				Namespace:   ns.Name,
				SubnetLabel: "subnet-foo",
			}
			invalidConfigData, err := yaml.Marshal(invalidConfig)
			Expect(err).NotTo(HaveOccurred())
			file, err := os.CreateTemp(GinkgoT().TempDir(), "invalidConfig.yaml")
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = file.Close()
			}()
			Expect(os.WriteFile(file.Name(), invalidConfigData, 0644)).To(Succeed())

			_, err = setup4(file.Name())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be 'key=value'"))
		})

		It("Setup6 should return error if less arguments are provided", func() {
			_, err := setup4()
			Expect(err).To(HaveOccurred())
		})

		It("Setup6 should return error if more arguments are provided", func() {
			_, err := setup4("foo", "bar")
			Expect(err).To(HaveOccurred())
		})

		It("Setup6 should return error if config file does not exist", func() {
			_, err := setup4("does-not-exist.yaml")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Plugin handler4", func() {
		It("Should break plugin chain, if not sending empty request ", func() {
			req, _ := dhcpv4.New()
			resp, _ := dhcpv4.NewReplyFromRequest(req)
			resp, stop := handler4(req, resp)
			Expect(stop).To(BeTrue())
			Expect(resp).To(BeNil())
		})

		It("should successfully handle request", func() {
			req, _ := dhcpv4.New()
			req.ClientHWAddr, _ = net.ParseMAC(machineWithIPAddressMACAddress)
			req.ClientIPAddr = net.ParseIP(privateIPV4Address)
			resp, _ := dhcpv4.NewReplyFromRequest(req)

			_, stop := handler4(req, resp)
			Expect(stop).To(BeFalse())
			Expect(resp).NotTo(BeNil())
		})
	})
})
