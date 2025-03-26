// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package metal

import (
	"net"
	"os"
	"strings"

	"github.com/ironcore-dev/fedhcp/internal/api"

	"gopkg.in/yaml.v2"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	"github.com/mdlayher/netx/eui64"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var _ = Describe("Endpoint", func() {
	ns := SetupTest()

	BeforeEach(func(ctx SpecContext) {
		By("Creating an IPAM IP objects")
		mac := machineWithIPAddressMACAddress
		m, err := net.ParseMAC(mac)
		Expect(err).NotTo(HaveOccurred())
		i := net.ParseIP(linkLocalIPV6Prefix)
		linkLocalIPV6Addr, err := eui64.ParseMAC(i, m)
		Expect(err).NotTo(HaveOccurred())

		sanitizedMAC := strings.ReplaceAll(mac, ":", "")
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
		Expect(k8sClient.Create(ctx, ipv6)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ipv6)

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

		Expect(k8sClient.Create(ctx, ipv4)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ipv4)

		Eventually(UpdateStatus(ipv4, func() {
			ipv4.Status.Reserved = ipv4Addr
		})).Should(Succeed())
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

	It("Should return an empty inventory for an empty list", func() {
		configFile := inventoryConfigFile
		data := api.MetalConfig{
			Inventories: []api.Inventory{
				{},
			},
			Filter: api.Filter{
				MacPrefix: []string{},
			},
		}
		configData, err := yaml.Marshal(data)
		Expect(err).NotTo(HaveOccurred())

		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

		i, err := loadConfig(file.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(i.Entries).To(BeEmpty())
	})

	It("Should return a valid inventory list with default name prefix for non-empty MAC address filter", func() {
		configFile := inventoryConfigFile
		data := api.MetalConfig{
			Filter: api.Filter{
				MacPrefix: []string{
					"aa:bb:cc:dd:ee:ff",
				},
			},
		}
		configData, err := yaml.Marshal(data)
		Expect(err).NotTo(HaveOccurred())

		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

		i, err := loadConfig(file.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(i.Entries).To(HaveKey("aa:bb:cc:dd:ee:ff"))
		pref := i.Entries["aa:bb:cc:dd:ee:ff"]
		Expect(pref).To(HavePrefix(defaultNamePrefix))
	})

	It("Should return an inventory list with custom name prefix for non-empty MAC address filter and set prefix", func() {
		configFile := inventoryConfigFile
		data := api.MetalConfig{
			NamePrefix: "server-",
			Filter: api.Filter{
				MacPrefix: []string{
					"aa:bb:cc:dd:ee:ff",
				},
			},
		}
		configData, err := yaml.Marshal(data)
		Expect(err).NotTo(HaveOccurred())

		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

		i, err := loadConfig(file.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(i.Entries).To(HaveKey("aa:bb:cc:dd:ee:ff"))
		pref := i.Entries["aa:bb:cc:dd:ee:ff"]
		Expect(pref).To(HavePrefix("server-"))
	})

	It("Should return a valid inventory list for a non-empty inventory section, precedence over MAC filter", func() {
		configFile := inventoryConfigFile
		data := api.MetalConfig{
			NamePrefix: "server-",
			Inventories: []api.Inventory{
				{
					Name:       "compute-1",
					MacAddress: "aa:bb:cc:dd:ee:ff",
				},
			},
			Filter: api.Filter{
				MacPrefix: []string{
					"aa:bb:cc:dd:ee:ff",
				},
			},
		}
		configData, err := yaml.Marshal(data)
		Expect(err).NotTo(HaveOccurred())

		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

		i, err := loadConfig(file.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(i.Entries).To(HaveKeyWithValue("aa:bb:cc:dd:ee:ff", "compute-1"))
	})

	It("Should create an endpoint for IPv6 DHCP request from a known machine with IP address", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(machineWithIPAddressMACAddress)
		ip := net.ParseIP(linkLocalIPV6Prefix)
		linkLocalIPV6Addr, _ := eui64.ParseMAC(ip, mac)

		req, _ := dhcpv6.NewMessage()
		req.MessageType = dhcpv6.MessageTypeRequest
		relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, linkLocalIPV6Addr)

		stub, _ := dhcpv6.NewMessage()
		stub.MessageType = dhcpv6.MessageTypeReply
		_, _ = handler6(relayedRequest, stub)

		endpoint := &metalv1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name: machineWithIPAddressName,
			},
		}

		Eventually(Object(endpoint)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", machineWithIPAddressMACAddress),
			HaveField("Spec.IP", metalv1alpha1.MustParseIP(linkLocalIPV6Addr.String()))))
		DeferCleanup(k8sClient.Delete, endpoint)
	})

	It("Should create an endpoint for IPv6 DHCP request from a known MAC prefix with IP address", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(machineWithIPAddressMACAddress)
		ip := net.ParseIP(linkLocalIPV6Prefix)
		linkLocalIPV6Addr, _ := eui64.ParseMAC(ip, mac)

		req, _ := dhcpv6.NewMessage()
		req.MessageType = dhcpv6.MessageTypeRequest
		relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, linkLocalIPV6Addr)

		configFile := inventoryConfigFile
		data := api.MetalConfig{
			NamePrefix:  "foobar-",
			Inventories: []api.Inventory{},
			Filter: api.Filter{
				MacPrefix: []string{machineWithIPAddressMACAddressPrefFilter},
			},
		}
		configData, err := yaml.Marshal(data)
		Expect(err).NotTo(HaveOccurred())

		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

		inventory, err = loadConfig(file.Name())
		Expect(err).NotTo(HaveOccurred())

		stub, _ := dhcpv6.NewMessage()
		stub.MessageType = dhcpv6.MessageTypeReply
		_, _ = handler6(relayedRequest, stub)

		epList := &metalv1alpha1.EndpointList{}
		Eventually(ObjectList(epList)).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", HavePrefix("foobar-")),
				HaveField("Spec.MACAddress", machineWithIPAddressMACAddress),
				HaveField("Spec.IP", metalv1alpha1.MustParseIP(linkLocalIPV6Addr.String())),
			))),
		))

		endpoint := &metalv1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name: epList.Items[0].Name,
			},
		}
		DeferCleanup(k8sClient.Delete, endpoint)
	})

	It("Should not return an IP address for a known machine without IP address", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(machineWithoutIPAddressMACAddress)

		ip, err := GetIPAMIPAddressForMACAddress(mac, ipamv1alpha1.CIPv6SubnetType)
		Eventually(err).ShouldNot(HaveOccurred())
		Eventually(ip).Should(BeNil())
	})

	It("Should not create an endpoint for IPv6 DHCP request from a known machine without IP address",
		func(ctx SpecContext) {
			mac, _ := net.ParseMAC(machineWithoutIPAddressMACAddress)
			ip := net.ParseIP(linkLocalIPV6Prefix)
			linkLocalIPV6Addr, _ := eui64.ParseMAC(ip, mac)

			req, _ := dhcpv6.NewMessage()
			req.MessageType = dhcpv6.MessageTypeRequest
			relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward,
				net.IPv6loopback, linkLocalIPV6Addr)

			stub, _ := dhcpv6.NewMessage()
			stub.MessageType = dhcpv6.MessageTypeReply
			_, _ = handler6(relayedRequest, stub)

			endpoint := &metalv1alpha1.Endpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name: machineWithoutIPAddressName,
				},
			}
			Eventually(Get(endpoint)).Should(Satisfy(apierrors.IsNotFound))
		})

	It("Should not create an endpoint for IPv6 DHCP request from a unknown machine", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(unknownMachineMACAddress)
		ip := net.ParseIP(linkLocalIPV6Prefix)
		linkLocalIPV6Addr, _ := eui64.ParseMAC(ip, mac)

		req, _ := dhcpv6.NewMessage()
		req.MessageType = dhcpv6.MessageTypeRequest
		relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, linkLocalIPV6Addr)

		stub, _ := dhcpv6.NewMessage()
		stub.MessageType = dhcpv6.MessageTypeReply
		_, _ = handler6(relayedRequest, stub)

		endpoint := &metalv1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name: machineWithoutIPAddressName,
			},
		}
		Eventually(Get(endpoint)).Should(Satisfy(apierrors.IsNotFound))
	})

	It("Should return and break plugin chain, if getting an IPv6 DHCP request directly (no relay)", func(ctx SpecContext) {
		req, _ := dhcpv6.NewMessage()
		req.MessageType = dhcpv6.MessageTypeRequest

		stub, _ := dhcpv6.NewMessage()
		stub.MessageType = dhcpv6.MessageTypeReply
		resp, breakChain := handler6(req, stub)

		Eventually(resp).Should(BeNil())
		Eventually(breakChain).Should(BeTrue())
	})

	It("Should create an endpoint for IPv4 DHCP request from a known machine with IP address", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(machineWithIPAddressMACAddress)

		req, _ := dhcpv4.NewDiscovery(mac)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		_, _ = handler4(req, stub)

		endpoint := &metalv1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name: machineWithIPAddressName,
			},
		}

		Eventually(Object(endpoint)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", machineWithIPAddressMACAddress),
			HaveField("Spec.IP", metalv1alpha1.MustParseIP(privateIPV4Address))))

		DeferCleanup(k8sClient.Delete, endpoint)
	})

	It("Should create an endpoint for IPv4 DHCP request from a known MAC prefix with IP address", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(machineWithIPAddressMACAddress)

		req, _ := dhcpv4.NewDiscovery(mac)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		configFile := inventoryConfigFile
		data := api.MetalConfig{
			NamePrefix:  "",
			Inventories: []api.Inventory{},
			Filter: api.Filter{
				MacPrefix: []string{machineWithIPAddressMACAddressPrefFilter},
			},
		}
		configData, err := yaml.Marshal(data)
		Expect(err).NotTo(HaveOccurred())

		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

		inventory, err = loadConfig(file.Name())
		Expect(err).NotTo(HaveOccurred())

		_, _ = handler4(req, stub)

		epList := &metalv1alpha1.EndpointList{}
		Eventually(ObjectList(epList)).Should(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Items", ContainElement(SatisfyAll(
				HaveField("ObjectMeta.Name", HavePrefix(defaultNamePrefix)),
				HaveField("Spec.MACAddress", machineWithIPAddressMACAddress),
				HaveField("Spec.IP", metalv1alpha1.MustParseIP(privateIPV4Address)),
			))),
		))

		endpoint := &metalv1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name: epList.Items[0].Name,
			},
		}
		DeferCleanup(k8sClient.Delete, endpoint)
	})

	It("Should not create an endpoint for IPv4 DHCP request from a known machine without IP address",
		func(ctx SpecContext) {
			mac, _ := net.ParseMAC(machineWithoutIPAddressMACAddress)

			req, _ := dhcpv4.NewDiscovery(mac)
			stub, _ := dhcpv4.NewReplyFromRequest(req)

			_, _ = handler4(req, stub)

			endpoint := &metalv1alpha1.Endpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name: machineWithoutIPAddressName,
				},
			}
			Eventually(Get(endpoint)).Should(Satisfy(apierrors.IsNotFound))
		})

	It("Should not create an endpoint for IPv6 DHCP request from a unknown machine", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(unknownMachineMACAddress)

		req, _ := dhcpv4.NewDiscovery(mac)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		_, _ = handler4(req, stub)

		endpoint := &metalv1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name: machineWithIPAddressName,
			},
		}
		Eventually(Get(endpoint)).Should(Satisfy(apierrors.IsNotFound))
	})
})
