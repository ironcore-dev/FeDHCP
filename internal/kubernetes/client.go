// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	metalv1alpha1 "github.com/ironcore-dev/metal-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var kubeClient client.Client

func InitClient() error {
	if kubeClient != nil {
		return nil
	}

	cfg := config.GetConfigOrDie()

	scheme := runtime.NewScheme()
	if err := metalv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("unable to add metalv1alpha1 to scheme: %w", err)
	}

	var err error
	kubeClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create controller runtime client: %w", err)
	}

	return nil
}

func SetClient(client *client.Client) {
	kubeClient = *client
}

func GetClient() client.Client {
	return kubeClient
}
