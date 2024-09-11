package metal

import (
	"github.com/insomniacslk/dhcp/dhcpv6"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var _ = Describe("Endpoint", func() {
	ns := SetupTest()

	BeforeEach(func(ctx SpecContext) {
		By("Creating an IPAM IP object")
		ipaddr, err := ipamv1alpha1.IPAddrFromString("fe80::1322:33ff:fe44:5566")
		Expect(err).NotTo(HaveOccurred())
		ip := &ipamv1alpha1.IP{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    ns.Name,
				GenerateName: "test-",
				Labels: map[string]string{
					"mac": "112233445566",
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

	It("Should create an endpoint for IPv6 request", func(ctx SpecContext) {
		req, _ := dhcpv6.NewMessage()
		req.MessageType = dhcpv6.MessageTypeRequest
		relayedRequest, _ := dhcpv6.EncapsulateRelay(req, dhcpv6.MessageTypeRelayForward, net.IPv6loopback, net.ParseIP("fe80::1322:33ff:fe44:5566"))

		stub, _ := dhcpv6.NewMessage()
		stub.MessageType = dhcpv6.MessageTypeReply
		_, _ = handler6(relayedRequest, stub)

		endpoint := &metalv1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		}

		Eventually(Object(endpoint)).Should(SatisfyAll(
			HaveField("Spec.MACAddress", "11:22:33:44:55:66"),
			HaveField("Spec.IP", metalv1alpha1.MustParseIP("fe80::1322:33ff:fe44:5566"))))
	})
})
