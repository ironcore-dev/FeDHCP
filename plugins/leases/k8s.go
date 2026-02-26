// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package leases

import (
	"context"
	"net"
	"time"

	fedhcpv1alpha1 "github.com/ironcore-dev/fedhcp/api/v1alpha1"
	"github.com/ironcore-dev/fedhcp/internal/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type k8sClient struct {
	client    client.Client
	namespace string
}

func newK8sClient(namespace string) *k8sClient {
	return &k8sClient{
		client:    kubernetes.GetClient(),
		namespace: namespace,
	}
}

func (k *k8sClient) applyLease(ctx context.Context, mac net.HardwareAddr, ip net.IP, name string, validLifetime time.Duration) error {
	now := metav1.Now()

	lease := &fedhcpv1alpha1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: k.namespace,
		},
	}

	result, err := controllerutil.CreateOrPatch(ctx, k.client, lease, func() error {
		lease.Spec.MAC = mac.String()
		lease.Spec.IP = ip.String()
		lease.Spec.Renewed = now
		lease.Spec.ExpiresAt = metav1.NewTime(now.Add(validLifetime))
		if lease.Spec.FirstSeen.IsZero() {
			lease.Spec.FirstSeen = now
		}
		return nil
	})
	if err != nil {
		return err
	}

	log.Infof("Lease %s/%s %s (MAC %s, IP %s)", k.namespace, name, result, mac, ip)
	return nil
}
