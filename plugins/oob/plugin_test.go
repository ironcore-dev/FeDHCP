// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/ironcore-dev/fedhcp/internal/api"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	"github.com/mdlayher/netx/eui64"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var _ = Describe("OOB Plugin", func() {
	var (
		testConfigPath string
		err            error
	)

	ns := SetupTest()

	BeforeEach(func(ctx SpecContext) {
		//Setup temporary test config file
		testConfigPath = "oob_config.yaml"
		config := &api.OOBConfig{
			Namespace:   ns.Name,
			SubnetLabel: subnetLabel,
		}
		configData, err := yaml.Marshal(config)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(testConfigPath, configData, 0644)
		Expect(err).NotTo(HaveOccurred())

		mac := machineWithIPAddressMACAddress
		m, err := net.ParseMAC(mac)
		Expect(err).NotTo(HaveOccurred())
		i := net.ParseIP(linkLocalIPV6Prefix)
		linkLocalIPV6Addr, err := eui64.ParseMAC(i, m)
		Expect(err).NotTo(HaveOccurred())

		sanitizedMAC := strings.Replace(mac, ":", "", -1)
		ipv6Addr, err := ipamv1alpha1.IPAddrFromString(linkLocalIPV6Addr.String())
		Expect(err).NotTo(HaveOccurred())
		ipv6 := &ipamv1alpha1.IP{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "test-",
				Labels: map[string]string{
					"mac": sanitizedMAC,
				},
			},
			Spec: ipamv1alpha1.IPSpec{
				Subnet: corev1.LocalObjectReference{
					Name: "foo",
				},
				IP: ipv6Addr,
			},
		}
		Expect(k8sClientTest.Create(ctx, ipv6)).To(Succeed())
		DeferCleanup(k8sClientTest.Delete, ipv6)

		Eventually(UpdateStatus(ipv6, func() {
			ipv6.Status.Reserved = ipv6Addr
		})).Should(Succeed())

		ipv4Addr, err := ipamv1alpha1.IPAddrFromString(privateIPV4Address)
		Expect(err).NotTo(HaveOccurred())
		ipv4 := &ipamv1alpha1.IP{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "test-",
				Labels: map[string]string{
					"mac": sanitizedMAC,
				},
			},
			Spec: ipamv1alpha1.IPSpec{
				Subnet: corev1.LocalObjectReference{
					Name: "bar",
				},
				IP: ipv4Addr,
			},
		}

		Expect(k8sClientTest.Create(ctx, ipv4)).To(Succeed())
		DeferCleanup(k8sClientTest.Delete, ipv4)

		Eventually(UpdateStatus(ipv4, func() {
			ipv4.Status.Reserved = ipv4Addr
		})).Should(Succeed())
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
			Expect(config.Namespace).To(Equal(ns.Name))
			Expect(config.SubnetLabel).To(Equal(subnetLabel))
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
				Namespace:   ns.Name,
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

	Describe("Plugin Setup4", func() {
		It("should successfully initialize the plugin with a valid config", func() {
			handler, err := setup4(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).NotTo(BeNil())
		})

		It("should return an error for invalid subnetLabel in the config", func() {
			invalidConfig := &api.OOBConfig{
				Namespace:   ns.Name,
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
	})

	Describe("Plugin handler6", func() {
		It("Should return and break plugin chain, if getting an IPv6 DHCP request directly (no relay)", func(ctx SpecContext) {
			req, _ := dhcpv6.NewMessage()
			req.MessageType = dhcpv6.MessageTypeRequest

			stub, _ := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			resp, breakChain := handler6(req, stub)

			Eventually(resp).Should(BeNil())
			Eventually(breakChain).Should(BeTrue())
		})

		It("should successfully handle request", func() {
			// subnet := &v1alpha1.Subnet{
			// 	ObjectMeta: v1.ObjectMeta{
			// 		Name:      "foo",
			// 		Namespace: ns.Name,
			// 	},
			// }

			// clientset, err2 := ipam.NewForConfig(cfg)
			// Expect(err2).NotTo(HaveOccurred())
			// createdSubnet, err1 := clientset.IpamV1alpha1().Subnets(ns.Name).Create(context.TODO(), subnet, v1.CreateOptions{})
			// Expect(err1).NotTo(HaveOccurred())
			// Expect(createdSubnet).NotTo(BeNil())

			// fmt.Printf("createdSubnet: %v", createdSubnet)

			mac, _ := net.ParseMAC(machineWithIPAddressMACAddress)
			ip := net.ParseIP(linkLocalIPV6Prefix)
			linkLocalIPV6Addr, _ := eui64.ParseMAC(ip, mac)

			req, _ := dhcpv6.NewMessage()
			req.MessageType = dhcpv6.MessageTypeRequest
			relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, linkLocalIPV6Addr)

			resp, err := handler6(relayedRequest, nil)
			fmt.Printf("error %v", err)
			Expect(err).To(BeNil())
			Expect(resp).NotTo(BeNil())
			respm, stop := resp.GetInnerMessage()
			Expect(stop).To(BeFalse())

			Expect(respm.MessageType).To(Equal(dhcpv6.MessageTypeRequest))
			Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).IPv6Addr.String()).To(Equal(linkLocalIPV6Prefix))
			Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).PreferredLifetime).To(Equal(24 * time.Hour))
			Expect(respm.Options.OneIANA().Options.Options[0].(*dhcpv6.OptIAAddress).ValidLifetime).To(Equal(24 * time.Hour))
		})

		It("should return an error for invalid subnetLabel in the config", func() {
			invalidConfig := &api.OOBConfig{
				Namespace:   ns.Name,
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
