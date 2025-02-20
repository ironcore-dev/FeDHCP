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
	whiteListMac         = "AA:BB:CC:DD:EE:FF"
	blackListMac         = "BB:BB:BB:BB:BB:CC"
	whiteListMacPrefix   = "AA:BB:CC"
	blackListMacPrefix   = "BB:BB:BB"
	otherBlackListMac    = "BB:BB:BB:BB:BB:DD"
	invalidMac           = "AABBCCDDEEFF"
	validMac             = "AA-BB-CC"
	unmatchedMac         = "00:11:22:33:44:55"
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
	config := &api.MACFilterConfig{
		WhiteList: []string{whiteListMacPrefix},
		BlackList: []string{blackListMacPrefix},
	}
	configData, err := yaml.Marshal(config)
	Expect(err).NotTo(HaveOccurred())
	err = os.WriteFile(testConfigPath, configData, 0644)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	fmt.Println("AfterSuite: Runs once after all tests")
	err = os.Remove(testConfigPath)
	Expect(err).NotTo(HaveOccurred())
})
