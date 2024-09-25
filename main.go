// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/coredhcp/coredhcp/config"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/coredhcp/coredhcp/plugins/autoconfigure"
	"github.com/coredhcp/coredhcp/plugins/dns"
	"github.com/coredhcp/coredhcp/plugins/example"
	"github.com/coredhcp/coredhcp/plugins/file"
	"github.com/coredhcp/coredhcp/plugins/leasetime"
	"github.com/coredhcp/coredhcp/plugins/mtu"
	"github.com/coredhcp/coredhcp/plugins/nbp"
	"github.com/coredhcp/coredhcp/plugins/netmask"
	"github.com/coredhcp/coredhcp/plugins/prefix"
	rangeplugin "github.com/coredhcp/coredhcp/plugins/range"
	"github.com/coredhcp/coredhcp/plugins/router"
	"github.com/coredhcp/coredhcp/plugins/searchdomains"
	"github.com/coredhcp/coredhcp/plugins/serverid"
	"github.com/coredhcp/coredhcp/plugins/sleep"
	"github.com/coredhcp/coredhcp/plugins/staticroute"
	"github.com/coredhcp/coredhcp/server"
	"github.com/ironcore-dev/fedhcp/internal/kubernetes"
	"github.com/ironcore-dev/fedhcp/plugins/bluefield"
	"github.com/ironcore-dev/fedhcp/plugins/httpboot"
	"github.com/ironcore-dev/fedhcp/plugins/ipam"
	"github.com/ironcore-dev/fedhcp/plugins/metal"
	"github.com/ironcore-dev/fedhcp/plugins/onmetal"
	"github.com/ironcore-dev/fedhcp/plugins/oob"
	"github.com/ironcore-dev/fedhcp/plugins/pxeboot"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var desiredPlugins = []*plugins.Plugin{
	&autoconfigure.Plugin,
	&dns.Plugin,
	&example.Plugin,
	&file.Plugin,
	&leasetime.Plugin,
	&mtu.Plugin,
	&nbp.Plugin,
	&netmask.Plugin,
	&prefix.Plugin,
	&rangeplugin.Plugin,
	&router.Plugin,
	&searchdomains.Plugin,
	&serverid.Plugin,
	&sleep.Plugin,
	&staticroute.Plugin,
	&bluefield.Plugin,
	&ipam.Plugin,
	&onmetal.Plugin,
	&oob.Plugin,
	&pxeboot.Plugin,
	&httpboot.Plugin,
	&metal.Plugin,
}

var (
	setupLog                   = ctrl.Log.WithName("setup")
	pluginsRequiringKubernetes = sets.New[string]("oob", "ipam", "metal")
)

func main() {
	var configFile string
	var listPlugins bool

	flag.StringVar(&configFile, "config", "", "config file")
	flag.BoolVar(&listPlugins, "list-plugins", false, "list plugins")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if listPlugins {
		for _, p := range desiredPlugins {
			fmt.Println(p.Name)
		}
		os.Exit(0)
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		setupLog.Error(err, "Failed to load configuration", "ConfigFile", configFile)
		os.Exit(1)
	}

	// register plugins
	for _, plugin := range desiredPlugins {
		if err := plugins.RegisterPlugin(plugin); err != nil {
			setupLog.Error(err, "Failed to register plugin", "Plugin", plugin.Name)
			os.Exit(1)
		}
	}

	// initialize kubernetes client, if needed
	if shouldSetupKubeClient(cfg) {
		if err := kubernetes.InitClient(); err != nil {
			setupLog.Error(err, "Failed to initialize kubernetes client")
			os.Exit(1)
		}
	}

	// start server
	srv, err := server.Start(cfg)
	if err != nil {
		setupLog.Error(err, "Failed to start server")
		os.Exit(1)
	}
	if err := srv.Wait(); err != nil {
		setupLog.Error(err, "Failed to wait server")
	}
}

func shouldSetupKubeClient(cfg *config.Config) bool {
	configuredPlugins := sets.Set[string]{}
	if cfg.Server4 != nil {
		for _, plugin := range cfg.Server4.Plugins {
			configuredPlugins.Insert(plugin.Name)
		}
	}
	if cfg.Server6 != nil {
		for _, plugin := range cfg.Server6.Plugins {
			configuredPlugins.Insert(plugin.Name)
		}
	}

	if configuredPlugins.HasAny(pluginsRequiringKubernetes.UnsortedList()...) {
		return true
	}

	return false
}
