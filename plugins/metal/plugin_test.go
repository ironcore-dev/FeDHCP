package metal

import (
	"encoding/json"
	"fmt"
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
		By("Creating an IPAM IP object")
		mac := machineWithIPAddressMACAddress
		m, err := net.ParseMAC(mac)
		Expect(err).NotTo(HaveOccurred())
		i := net.ParseIP(linkLocalIPV6Prefix)
		linkLocalIPV6Addr, err := eui64.ParseMAC(i, m)
		Expect(err).NotTo(HaveOccurred())

		sanitizedMAC := strings.Replace(mac, ":", "", -1)
		ipaddr, err := ipamv1alpha1.IPAddrFromString(linkLocalIPV6Addr.String())
		Expect(err).NotTo(HaveOccurred())
		ip := &ipamv1alpha1.IP{
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
				IP: ipaddr,
			},
		}
		Expect(k8sClient.Create(ctx, ip)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ip)

		Eventually(UpdateStatus(ip, func() {
			ip.Status.Reserved = ipaddr
		})).Should(Succeed())
	})

	/* parametrization */
	It("Should return error if less arguments are provided", func() {
		err := loadConfig()
		Expect(err).NotTo(BeNil())
	})

	It("Should return error if more arguments are provided", func() {
		err := loadConfig("foo", "bar")
		Expect(err).NotTo(BeNil())
	})

	It("Should return error if config file does not exist", func() {
		Expect(func() error {
			err := loadConfig("do-not-exist.json")
			return err
		}).ShouldNot(BeNil())
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

		ip, err := GetIPForMACAddress(mac)
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
			Name: "foo",
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
})
