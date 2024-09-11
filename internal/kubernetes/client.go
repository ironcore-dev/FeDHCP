package kubernetes

import (
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func NewClient(scheme *runtime.Scheme) (client.Client, error) {
	cfg := config.GetConfigOrDie()
	cl, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create controller runtime client: %w", err)
	}

	return cl, nil
}
