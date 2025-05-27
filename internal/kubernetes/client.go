// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"

	"k8s.io/client-go/kubernetes/scheme"

	ipamv1alpha1 "github.com/ironcore-dev/ipam/api/ipam/v1alpha1"
	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	kubeClient client.Client
	cfg        *rest.Config
)

func init() {
	utilruntime.Must(ipamv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(metalv1alpha1.AddToScheme(scheme.Scheme))
}

func InitClient() error {
	cfg = config.GetConfigOrDie()
	var err error
	kubeClient, err = client.New(cfg, client.Options{})
	if err != nil {
		return fmt.Errorf("failed to create controller runtime client: %w", err)
	}

	return nil
}

func SetClient(client *client.Client) {
	kubeClient = *client
}

func GetClient() client.Client { return kubeClient }

func GetConfig() *rest.Config { return cfg }
