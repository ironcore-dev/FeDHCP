// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package ipam

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ironcore-dev/controller-utils/modutils"
	"github.com/ironcore-dev/fedhcp/internal/api"
	"github.com/ironcore-dev/fedhcp/internal/kubernetes"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
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
	//+kubebuilder:scaffold:imports
)

const (
	pollingInterval                = 50 * time.Millisecond
	eventuallyTimeout              = 3 * time.Second
	consistentlyDuration           = 1 * time.Second
	linkLocalIPV6Prefix            = "fe80::"
	machineWithIPAddressMACAddress = "11:22:33:44:55:66"
	machineWithMacAddress          = "11:22:33:44:55:77"
	ipamConfigFile                 = "config.yaml"
)

var (
	cfg           *rest.Config
	k8sClientTest client.Client
	testEnv       *envtest.Environment
)

func TestBluefield(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)
	RunSpecs(t, "IPAM Plugin Suite")
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
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	DeferCleanup(testEnv.Stop)

	Expect(ipamv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme
	k8sClientTest, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClientTest).NotTo(BeNil())

	// set komega client
	SetClient(k8sClientTest)

	// assign global k8s client in plugin
	kubernetes.SetClient(&k8sClientTest)
	kubernetes.SetConfig(cfg)
})

func SetupTest() *corev1.Namespace {
	ns := &corev1.Namespace{}

	BeforeEach(func(ctx SpecContext) {
		*ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
		}

		Expect(k8sClientTest.Create(ctx, ns)).To(Succeed(), "failed to create test namespace")
		DeferCleanup(k8sClientTest.Delete, ns)

		configFile := ipamConfigFile
		data := &api.IPAMConfig{
			Namespace: ns.Name,
			Subnets:   []string{"ipam-subnet1", "ipam-subnet2"},
		}

		configData, err := yaml.Marshal(data)
		Expect(err).NotTo(HaveOccurred())

		file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			_ = file.Close()
		}()
		Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

		config, err := loadConfig(file.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Namespace).To(Equal(ns.Name))
		Expect(config.Subnets[0]).To(Equal("ipam-subnet1"))
		Expect(config.Subnets[1]).To(Equal("ipam-subnet2"))
	})

	return ns
}
