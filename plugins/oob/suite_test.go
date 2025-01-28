// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ironcore-dev/controller-utils/modutils"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/ironcore-dev/fedhcp/internal/kubernetes"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	"github.com/mdlayher/netx/eui64"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

const (
	pollingInterval                = 50 * time.Millisecond
	eventuallyTimeout              = 3 * time.Second
	consistentlyDuration           = 1 * time.Second
	oobConfigFile                  = "config.yaml"
	unknownMachineMACAddress       = "11:11:11:11:11:11"
	linkLocalIPV6Prefix            = "fe80::"
	machineWithIPAddressMACAddress = "11:22:33:44:55:66"
	privateIPV4Address             = "192.168.1.11"
)

var (
	cfg               *rest.Config
	k8sClientTest     client.Client
	testEnv           *envtest.Environment
	ns                corev1.Namespace
	testConfigPath    string
	linkLocalIPV6Addr net.IP
)

func TestOOB(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)

	RunSpecs(t, "OOB Plugin Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			modutils.Dir("github.com/ironcore-dev/ipam", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.30.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	DeferCleanup(testEnv.Stop)

	Expect(ipamv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(metalv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClientTest, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClientTest).NotTo(BeNil())

	// set komega client
	SetClient(k8sClientTest)

	// assign global k8s client in plugin
	kubernetes.SetClient(&k8sClientTest)
	kubernetes.SetConfig(cfg)

	setupTest6()
	setupTest4()
})

func setupTest6() {
	ns = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-6",
		},
	}
	Expect(k8sClientTest.Create(context.Background(), &ns)).To(Succeed(), "failed to create test namespace")

	configFile := oobConfigFile
	data := &api.OOBConfig{
		Namespace:   ns.Name,
		SubnetLabel: "subnet=foo",
	}

	configData, err := yaml.Marshal(data)
	Expect(err).NotTo(HaveOccurred())

	file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	testConfigPath = file.Name()
	Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

	handler, err := setup6(file.Name())
	Expect(err).NotTo(HaveOccurred())
	Expect(handler).NotTo(BeNil())

	subnet6 := &ipamv1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "foo-v6",
			Labels: map[string]string{
				"subnet": "foo",
			},
		},
	}

	cidr := &ipamv1alpha1.CIDR{
		Net: netip.MustParsePrefix("fe80::/64"),
	}
	Expect(k8sClientTest.Create(context.Background(), subnet6)).To(Succeed())
	DeferCleanup(k8sClientTest.Delete, subnet6)

	Eventually(UpdateStatus(subnet6, func() {
		subnet6.Status.Type = ipamv1alpha1.CIPv6SubnetType
		subnet6.Status.Reserved = cidr
	})).Should(Succeed())

	By("creating an IPAM IP")
	m, err := net.ParseMAC(machineWithIPAddressMACAddress)
	Expect(err).NotTo(HaveOccurred())
	i := net.ParseIP(linkLocalIPV6Prefix)
	linkLocalIPV6Addr, err = eui64.ParseMAC(i, m)
	Expect(err).NotTo(HaveOccurred())

	sanitizedMAC := strings.Replace(machineWithIPAddressMACAddress, ":", "", -1)
	ipv6Addr, err := ipamv1alpha1.IPAddrFromString(linkLocalIPV6Addr.String())
	Expect(err).NotTo(HaveOccurred())
	ipv6 := &ipamv1alpha1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "test-ipv6",
			Labels: map[string]string{
				"mac": sanitizedMAC,
			},
		},
		Spec: ipamv1alpha1.IPSpec{
			Subnet: corev1.LocalObjectReference{
				Name: "foo-v6",
			},
			IP: ipv6Addr,
		},
	}

	Expect(k8sClientTest.Create(context.Background(), ipv6)).To(Succeed())
	DeferCleanup(k8sClientTest.Delete, ipv6)

	Eventually(UpdateStatus(ipv6, func() {
		ipv6.Status.Reserved = ipv6.Spec.IP
	})).Should(Succeed())
}

func setupTest4() {
	configFile := oobConfigFile
	data := &api.OOBConfig{
		Namespace:   ns.Name,
		SubnetLabel: "subnet=foo",
	}

	configData, err := yaml.Marshal(data)
	Expect(err).NotTo(HaveOccurred())

	file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()

	Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

	handler, err := setup4(file.Name())
	Expect(err).NotTo(HaveOccurred())
	Expect(handler).NotTo(BeNil())
	subnet4 := &ipamv1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "foo-v4",
			Labels: map[string]string{
				"subnet": "foo",
			},
		},
	}

	cidr := &ipamv1alpha1.CIDR{
		Net: netip.MustParsePrefix("192.168.1.0/24"),
	}
	Expect(k8sClientTest.Create(context.Background(), subnet4)).To(Succeed())
	DeferCleanup(k8sClientTest.Delete, subnet4)

	Eventually(UpdateStatus(subnet4, func() {
		subnet4.Status.Type = ipamv1alpha1.CIPv4SubnetType
		subnet4.Status.Reserved = cidr
	})).Should(Succeed())

	By("creating an IPAM IPv4")
	sanitizedMAC := strings.Replace(machineWithIPAddressMACAddress, ":", "", -1)
	ipv4Addr, err := ipamv1alpha1.IPAddrFromString(privateIPV4Address)
	Expect(err).NotTo(HaveOccurred())
	ipv4 := &ipamv1alpha1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "test-ipv4",
			Labels: map[string]string{
				"mac": sanitizedMAC,
			},
		},
		Spec: ipamv1alpha1.IPSpec{
			Subnet: corev1.LocalObjectReference{
				Name: "foo-v4",
			},
			IP: ipv4Addr,
		},
	}

	Expect(k8sClientTest.Create(context.Background(), ipv4)).To(Succeed())
	DeferCleanup(k8sClientTest.Delete, ipv4)

	Eventually(UpdateStatus(ipv4, func() {
		ipv4.Status.Reserved = ipv4.Spec.IP
	})).Should(Succeed())
}
