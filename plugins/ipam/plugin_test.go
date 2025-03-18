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

		ipv6, linkLocalIPV6Addr = createTestIPAMIP(machineWithIPAddressMACAddress, *ns)
		Expect(ipv6).NotTo(BeNil())
		Expect(linkLocalIPV6Addr).NotTo(BeNil())

		Expect(k8sClientTest.Create(ctx, ipv6)).To(Succeed())
		DeferCleanup(k8sClientTest.Delete, ipv6)

		Eventually(UpdateStatus(ipv6, func() {
			ipv6.Status.Reserved = ipv6.Spec.IP
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

		It("should not return an error for empty IPAM config", func() {
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
			req, _ := dhcpv6.NewMessage()
			req.MessageType = dhcpv6.MessageTypeRequest
			relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, linkLocalIPV6Addr)

			stub, err := dhcpv6.NewMessage()
			Expect(err).ToNot(HaveOccurred())
			resp, stop := handler6(relayedRequest, stub)
			Expect(stop).To(BeFalse())
			Expect(resp).NotTo(BeNil())
		})
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

		It("return formatted string, if valid ipv6", func() {
			longIP := getLongIPv6(net.ParseIP("fe80::"))
			Expect(longIP).To(Equal("fe80-0000-0000-0000-0000-0000-0000-0000"))
		})

		It("return panic, if invalid ipv6", func() {
			Expect(func() {
				getLongIPv6(net.ParseIP("fe80::bcd::ccd::bcd"))
			}).To(Panic())
		})

		It("return pretty formatted string for ipamv1alpha1.IPSpec", func() {
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

var _ = Describe("K8s Client tests", func() {
	var (
		linkLocalIPV6Addr net.IP
		ipv6              *ipamv1alpha1.IP
		k8sClient         *K8sClient
		err               error
	)

	ns := SetupTest()

	BeforeEach(func() {
		By("creating an IPAM IP")
		ipv6, linkLocalIPV6Addr = createTestIPAMIP(machineWithMacAddress, *ns)
		Expect(ipv6).NotTo(BeNil())
		Expect(linkLocalIPV6Addr).NotTo(BeNil())

		k8sClient, err = NewK8sClient(ns.Name, []string{"foo"})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())
	})

	It("should successfully match the subnet", func() {
		subnet, err := k8sClient.getMatchingSubnet("foo", linkLocalIPV6Addr)
		Expect(err).NotTo(HaveOccurred())
		Expect(subnet).To(BeNil())
	})

	It("should return error if subnet not matched", func() {
		subnet, err := k8sClient.getMatchingSubnet("random-subnet", linkLocalIPV6Addr)
		Expect(err).ToNot(HaveOccurred())
		Expect(subnet).To(BeNil())
	})

	It("should return (nil, nil) if CIDR mismatch", func() {
		subnet, err := k8sClient.getMatchingSubnet("foo", net.IP("11:22:33:44"))
		Expect(err).ToNot(HaveOccurred())
		Expect(subnet).To(BeNil())
	})

	It("should successfully return IPAM IP for machine with mac address", func() {
		ip, err := k8sClient.prepareCreateIpamIP("foo", linkLocalIPV6Addr, net.HardwareAddr(machineWithIPAddressMACAddress))
		Expect(err).NotTo(HaveOccurred())
		Expect(ip).NotTo(BeNil())
	})

	It("should return error failed to parse IP if invalid ip prefix", func() {
		m, err := net.ParseMAC(machineWithIPAddressMACAddress)
		Expect(err).NotTo(HaveOccurred())
		i := net.ParseIP("fe80??::")
		linkLocalIPV6Addr, err := eui64.ParseMAC(i, m)
		Expect(err).To(HaveOccurred())

		ip, err := k8sClient.prepareCreateIpamIP("foo", linkLocalIPV6Addr, net.HardwareAddr(machineWithIPAddressMACAddress))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse IP"))
		Expect(ip).To(BeNil())
	})

	It("should successfully return with (nil, nil) if IP already exists in subnet", func() {
		m, err := net.ParseMAC(machineWithIPAddressMACAddress)
		Expect(err).NotTo(HaveOccurred())
		i := net.ParseIP("fe80::")
		linkLocalIPV6Addr, err := eui64.ParseMAC(i, m)
		Expect(err).NotTo(HaveOccurred())

		ip, err := k8sClient.prepareCreateIpamIP("foo", linkLocalIPV6Addr, net.HardwareAddr(machineWithIPAddressMACAddress))
		Expect(err).NotTo(HaveOccurred())
		Expect(ip).NotTo(BeNil())

		createIPError := k8sClient.doCreateIpamIP(ip)
		Expect(createIPError).NotTo(HaveOccurred())

		sameip, err := k8sClient.prepareCreateIpamIP("foo", linkLocalIPV6Addr, net.HardwareAddr(machineWithIPAddressMACAddress))
		Expect(err).ToNot(HaveOccurred())
		Expect(sameip).To(BeNil())
	})

	It("should successfully create IPAM IP for machine with mac address", func() {
		createIPError := k8sClient.doCreateIpamIP(ipv6)
		Expect(createIPError).NotTo(HaveOccurred())
	})

	It("should successfully create IPAM IP for machine", func() {
		createIPError := k8sClient.createIpamIP(linkLocalIPV6Addr, net.HardwareAddr(machineWithIPAddressMACAddress))
		Expect(createIPError).NotTo(HaveOccurred())
	})

	It("should return timeout error, if IPAM IP not deleted", func() {
		err := k8sClient.waitForDeletion(ipv6)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("timeout reached, IP not deleted"))
	})
})

func createTestIPAMIP(mac string, ns corev1.Namespace) (*ipamv1alpha1.IP, net.IP) {
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
			Namespace: ns.Name,
			Name:      "test-ip",
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
	return ipv6, linkLocalIPV6Addr
}
