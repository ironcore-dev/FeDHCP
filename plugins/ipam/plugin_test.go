// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ipam

import (
	"net"
	"os"
	"strings"

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

var _ = Describe("IPAM Plugin", func() {
	var (
		testConfigPath    string
		err               error
		linkLocalIPV6Addr net.IP
		ipv6              *ipamv1alpha1.IP
	)

	ns := SetupTest()

	BeforeEach(func(ctx SpecContext) {
		// Setup temporary test config file
		testConfigPath = "test_config.yaml"
		config := &api.IPAMConfig{
			Namespace: ns.Name,
			Subnets:   []string{"ipam-subnet1", "ipam-subnet2"},
		}
		configData, err := yaml.Marshal(config)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(testConfigPath, configData, 0644)
		Expect(err).NotTo(HaveOccurred())

		mac := machineWithIPAddressMACAddress
		m, err := net.ParseMAC(mac)
		Expect(err).NotTo(HaveOccurred())
		i := net.ParseIP(linkLocalIPV6Prefix)
		linkLocalIPV6Addr, err = eui64.ParseMAC(i, m)
		Expect(err).NotTo(HaveOccurred())

		sanitizedMAC := strings.Replace(mac, ":", "", -1)
		ipv6Addr, err := ipamv1alpha1.IPAddrFromString(linkLocalIPV6Addr.String())
		Expect(err).NotTo(HaveOccurred())
		ipv6 = &ipamv1alpha1.IP{
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
			Expect(config.Subnets[len(config.Subnets)-1]).To(Equal("ipam-subnet2"))
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

		It("should not return an error for empty ipam config", func() {
			invalidConfig := &api.IPAMConfig{}
			invalidConfigData, err := yaml.Marshal(invalidConfig)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(testConfigPath, invalidConfigData, 0644)
			Expect(err).NotTo(HaveOccurred())

			_, err = setup6(testConfigPath)
			Expect(err).NotTo(HaveOccurred())
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
			// //sanitizedMAC := strings.Replace(mac, ":", "", -1)
			// ipv6Addr, _ := ipamv1alpha1.IPAddrFromString(linkLocalIPV6Addr.String())

			// ipv6 := &ipamv1alpha1.IP{
			// 	ObjectMeta: metav1.ObjectMeta{
			// 		Namespace:    ns.Name,
			// 		GenerateName: "test-",
			// 	},
			// 	Spec: ipamv1alpha1.IPSpec{
			// 		Subnet: corev1.LocalObjectReference{
			// 			Name: "foo",
			// 		},
			// 		IP: ipv6Addr,
			// 	},
			// }

			// err3 := k8sClientTest.Create(context.TODO(), ipv6)
			// Expect(err3).To(BeNil())

			// createdSubnet := &ipamv1alpha1.Subnet{
			// 	ObjectMeta: metav1.ObjectMeta{
			// 		Name:      "foo",
			// 		Namespace: ns.Name,
			// 	},
			// }

			// err5 := k8sClientTest.Create(context.TODO(), createdSubnet)
			// Expect(err5).To(BeNil())

			// subnet := &ipamv1alpha1.Subnet{
			// 	ObjectMeta: metav1.ObjectMeta{
			// 		Name:      "foo",
			// 		Namespace: ns.Name,
			// 	},
			// }
			// existingSubnet := subnet.DeepCopy()
			// err4 := k8sClientTest.Get(context.TODO(), client.ObjectKeyFromObject(subnet), existingSubnet)
			// Expect(err4).To(BeNil())

			//Expect(k8sClientTest.Create(context.TODO(), ipv6)).To(Succeed())
			// clientset, err2 := ipam.NewForConfig(cfg)
			// Expect(err2).NotTo(HaveOccurred())
			// createdSubnet, err1 := clientset.IpamV1alpha1().Subnets(ns.Name).Create(context.TODO(), subnet, v1.CreateOptions{})
			// Expect(err1).NotTo(HaveOccurred())
			// Expect(createdSubnet).NotTo(BeNil())

			//fmt.Printf("createdSubnet: %v", createdSubnet)

			// mac, _ := net.ParseMAC(machineWithIPAddressMACAddress)
			// ip := net.ParseIP(linkLocalIPV6Prefix)
			// linkLocalIPV6Addr, _ := eui64.ParseMAC(ip, mac)

			req, _ := dhcpv6.NewMessage()
			req.MessageType = dhcpv6.MessageTypeRequest
			relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, linkLocalIPV6Addr)

			stub, err := dhcpv6.NewMessage()
			Expect(err).To(BeNil())
			resp, stop := handler6(relayedRequest, stub)
			Expect(stop).To(BeFalse())
			Expect(resp).NotTo(BeNil())
		})
	})

	Describe("K8s Client tests", func() {
		It("should successfully match the subnet", func() {
			k8sClient, err := NewK8sClient(ns.Name, []string{"foo"})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient).NotTo(BeNil())

			subnet, err := k8sClient.getMatchingSubnet("foo", linkLocalIPV6Addr)
			Expect(err).NotTo(HaveOccurred())
			Expect(subnet).To(BeNil())
		})

		// It("should successfully match the subnet", func() {
		// 	err := k8sClient.doCreateIpamIP(ipv6)
		// 	Expect(err).NotTo(HaveOccurred())
		// })
	})

	Describe("Common tests", func() {
		It("return true checks the ip in CIDR", func() {
			checkIP := checkIPv6InCIDR(linkLocalIPV6Addr, "fe80::/64")
			Expect(checkIP).To(BeTrue())
		})

		It("return false, if invalid CIDR", func() {
			checkIP := checkIPv6InCIDR(linkLocalIPV6Addr, "fe80::")
			Expect(checkIP).To(BeFalse())
		})

		It("return formrted string, if valid ipv6", func() {
			longIP := getLongIPv6(net.ParseIP("fe80::"))
			Expect(longIP).To(Equal("fe80-0000-0000-0000-0000-0000-0000-0000"))
		})

		It("return panic, if invalid ipv6", func() {
			Expect(func() {
				getLongIPv6(net.ParseIP("fe80::bcd::ccd::bcd"))
			}).To(Panic())
		})

		It("return pretty fromated string for ipamv1alpha1.IPSpec", func() {
			ipv6Addr, err := ipamv1alpha1.IPAddrFromString(linkLocalIPV6Addr.String())
			Expect(err).NotTo(HaveOccurred())
			ipv6 := &ipamv1alpha1.IP{
				Spec: ipamv1alpha1.IPSpec{
					Subnet: corev1.LocalObjectReference{
						Name: "foo",
					},
					IP: ipv6Addr,
				},
			}
			format := prettyFormat(ipv6.Spec)
			Expect(format).ShouldNot(BeEmpty())
		})
	})
})
