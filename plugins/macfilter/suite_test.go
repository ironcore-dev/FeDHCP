// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package macfilter

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ironcore-dev/fedhcp/internal/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

const (
	pollingInterval      = 50 * time.Millisecond
	eventuallyTimeout    = 3 * time.Second
	consistentlyDuration = 1 * time.Second
	testConfigPath       = "test_config.yaml"
	allowListMac         = "AA:AA:AA:AA:AA:AA"
	denyListMac          = "BB:BB:BB:BB:BB:BB"
	allowListMacPrefix   = "AA:AA:AA"
	denyListMacPrefix    = "BB:BB:BB"
	unmatchedMac         = "FF:FF:FF:FF:FF:FF"
)

func TestMACFilter(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Macfilter Plugin Suite")
}

var _ = BeforeSuite(func() {
	fmt.Println("BeforeSuite: Runs once before all tests")
	configFile := testConfigPath
	config := &api.MACFilterConfig{
		AllowList: []string{allowListMacPrefix},
		DenyList:  []string{denyListMacPrefix},
	}
	configData, err := yaml.Marshal(config)
	Expect(err).NotTo(HaveOccurred())

	file, err := os.CreateTemp(GinkgoT().TempDir(), configFile)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		_ = file.Close()
	}()
	Expect(os.WriteFile(file.Name(), configData, 0644)).To(Succeed())

	_, err = setup6(file.Name())
	Expect(err).NotTo(HaveOccurred())
	Expect(macFilterConfig).NotTo(BeNil())
	Expect(macFilterConfig.AllowList[0]).To(Equal(allowListMacPrefix))
	Expect(macFilterConfig.DenyList[0]).To(Equal(denyListMacPrefix))
})
