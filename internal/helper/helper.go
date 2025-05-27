// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/ironcore-dev/fedhcp/internal/kubernetes"
	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Configuration struct {
	IpPollingInterval time.Duration
	IpPollingTimeout  time.Duration
}

var Config Configuration

func WaitForIPDeletion(ctx context.Context, ipamIP *ipamv1alpha1.IP) error {
	cl := kubernetes.GetClient()

	if err := wait.PollUntilContextTimeout(ctx, Config.IpPollingInterval, Config.IpPollingTimeout, true, func(ctx context.Context) (bool, error) {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(ipamIP), ipamIP); err != nil {
			if !apierrors.IsNotFound(err) {
				return false, err
			} else {
				// IP is deleted
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("timeout deleting IP %s: %w", client.ObjectKeyFromObject(ipamIP), err)
	}

	return nil
}

func WaitForIPCreation(ctx context.Context, ipamIP *ipamv1alpha1.IP) (*ipamv1alpha1.IP, error) {
	cl := kubernetes.GetClient()

	if err := wait.PollUntilContextTimeout(ctx, Config.IpPollingInterval, Config.IpPollingTimeout, true, func(ctx context.Context) (bool, error) {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(ipamIP), ipamIP); err != nil {
			return false, err
		}
		if ipamIP.Status.State == ipamv1alpha1.FinishedIPState {
			return true, nil
		} else {
			return false, nil
		}
	}); err != nil {
		return nil, fmt.Errorf("timeout getting IP %s: %w", client.ObjectKeyFromObject(ipamIP), err)
	}

	return ipamIP, nil
}

func PrettyFormat(ipSpec interface{}, log *logrus.Entry) string {
	// Marshal the struct into a JSON string with pretty printing
	jsonBytes, err := json.MarshalIndent(ipSpec, "", "  ")
	if err != nil {
		log.Errorf("Error marshalling JSON: %v", err)
	}

	// Convert the JSON bytes to a string and print
	return string(jsonBytes)
}

func CheckIPInCIDR(ip net.IP, cidrStr string, log *logrus.Entry) bool {
	// Parse the CIDR string
	_, cidrNet, err := net.ParseCIDR(cidrStr)
	if err != nil {
		log.Errorf("Error parsing CIDR: %v\n", err)
		return false
	}

	// Check if the CIDR contains the IP
	return cidrNet.Contains(ip)
}
