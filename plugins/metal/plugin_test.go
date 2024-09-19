// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package metal

import (
	"encoding/json"
	"fmt"
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
	"k8s.io/apimachinery/pkg/types"
	"net"
	"os"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"strings"
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

	/* parametrization */
	It("Setup6 should return error if less arguments are provided", func() {
		_, err := setup6()
		Expect(err).NotTo(BeNil())
	})

	It("Setup6 should return error if more arguments are provided", func() {
		_, err := setup6("foo", "bar")
		Expect(err).NotTo(BeNil())
	})

	It("Setup6 should return error if config file does not exist", func() {
		_, err := setup6("does-not-exist.json")
		Expect(err).NotTo(BeNil())
	})

	It("Setup4 should return error if less arguments are provided", func() {
		_, err := setup4()
		Expect(err).NotTo(BeNil())
	})

	It("Setup4 should return error if more arguments are provided", func() {
		_, err := setup4("foo", "bar")
		Expect(err).NotTo(BeNil())
	})

	It("Setup4 should return error if config file does not exist", func() {
		_, err := setup4("does-not-exist.json")
		Expect(err).NotTo(BeNil())
	})

	It("Should return empty machine list if the config file is malformed", func() {
		// empty the machine map
		machineMap = make(map[string]string)

		malformedJson := "malformed.json"
		// Create a map with key-value pairs
		data := map[string]string{
			"foo":  "bar",
			"fizz": "buzz",
		}

		// Create a JSON file
		file, err := os.Create(malformedJson)
		if err != nil {
			fmt.Println("Error creating file:", err)
			return
		}
		defer os.Remove(malformedJson)
		defer file.Close()

		// Encode the map as JSON and write it to the file
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ") // Optional: to pretty-print the JSON with indentation
		err = encoder.Encode(data)
		if err != nil {
			fmt.Println("Error encoding JSON:", err)
			return
		}

		err = loadConfig(malformedJson)
		Expect(machineMap).To(BeEmpty())
	})

	/* IPv6 */
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

	It("Should not return an IP address for a known machine without IP address", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(machineWithoutIPAddressMACAddress)

		ip, err := GetIPForMACAddress(mac, ipamv1alpha1.CIPv6SubnetType)
		Eventually(err).Should(BeNil())
		Eventually(ip).Should(BeNil())
	})

	It("Should not create an endpoint for IPv6 DHCP request from a known machine without IP address", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(machineWithoutIPAddressMACAddress)
		ip := net.ParseIP(linkLocalIPV6Prefix)
		linkLocalIPV6Addr, _ := eui64.ParseMAC(ip, mac)

		req, _ := dhcpv6.NewMessage()
		req.MessageType = dhcpv6.MessageTypeRequest
		relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, linkLocalIPV6Addr)

		stub, _ := dhcpv6.NewMessage()
		stub.MessageType = dhcpv6.MessageTypeReply
		_, _ = handler6(relayedRequest, stub)

		epName := types.NamespacedName{
			Name: machineWithoutIPAddressName,
		}
		endpoint := &metalv1alpha1.Endpoint{}

		Eventually(func() error {
			return k8sClient.Get(ctx, epName, endpoint)
		}).ShouldNot(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, epName, endpoint)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
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

		epName := types.NamespacedName{
			Name: machineWithIPAddressName,
		}
		endpoint := &metalv1alpha1.Endpoint{}

		Eventually(func() error {
			return k8sClient.Get(ctx, epName, endpoint)
		}).ShouldNot(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, epName, endpoint)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
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

	/* IPv4 */
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

	It("Should not create an endpoint for IPv4 DHCP request from a known machine without IP address", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(machineWithoutIPAddressMACAddress)

		req, _ := dhcpv4.NewDiscovery(mac)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		_, _ = handler4(req, stub)

		epName := types.NamespacedName{
			Name: machineWithoutIPAddressName,
		}
		endpoint := &metalv1alpha1.Endpoint{}

		Eventually(func() error {
			return k8sClient.Get(ctx, epName, endpoint)
		}).ShouldNot(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, epName, endpoint)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
	})

	It("Should not create an endpoint for IPv6 DHCP request from a unknown machine", func(ctx SpecContext) {
		mac, _ := net.ParseMAC(unknownMachineMACAddress)

		req, _ := dhcpv4.NewDiscovery(mac)
		stub, _ := dhcpv4.NewReplyFromRequest(req)

		_, _ = handler4(req, stub)

		epName := types.NamespacedName{
			Name: machineWithIPAddressName,
		}
		endpoint := &metalv1alpha1.Endpoint{}

		Eventually(func() error {
			return k8sClient.Get(ctx, epName, endpoint)
		}).ShouldNot(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, epName, endpoint)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
	})
})
