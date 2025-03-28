// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"

	"github.com/coredhcp/coredhcp/logger"

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
	"github.com/ironcore-dev/fedhcp/plugins/macfilter"
	"github.com/ironcore-dev/fedhcp/plugins/metal"
	"github.com/ironcore-dev/fedhcp/plugins/onmetal"
	"github.com/ironcore-dev/fedhcp/plugins/oob"
	"github.com/ironcore-dev/fedhcp/plugins/pxeboot"
	"github.com/ironcore-dev/fedhcp/plugins/ztp"
	"k8s.io/apimachinery/pkg/util/sets"
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
	&macfilter.Plugin,
	&ztp.Plugin,
}

var (
	log                        = logger.GetLogger("main")
	pluginsRequiringKubernetes = sets.New[string]("oob", "ipam", "metal")
)

func main() {
	var configFile, logLevel string
	var listPlugins bool

	flag.StringVar(&configFile, "config", "", "config file")
	flag.BoolVar(&listPlugins, "list-plugins", false, "list plugins")
	flag.StringVar(&logLevel, "loglevel", "info", "log level (debug, info, warning, error, fatal, panic)")
	flag.Parse()

	if listPlugins {
		for _, p := range desiredPlugins {
			fmt.Println(p.Name)
		}
		os.Exit(0)
	}

	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		fmt.Println("Invalid log level specified: ", err)
		os.Exit(1)
	}
	log.Logger.SetLevel(level)

	cfg, err := config.Load(configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration %s: %v", configFile, err)
		os.Exit(1)
	}

	// register plugins
	for _, plugin := range desiredPlugins {
		if err := plugins.RegisterPlugin(plugin); err != nil {
			log.Fatalf("Failed to register plugin '%s': %v", plugin.Name, err)
			os.Exit(1)
		}
	}

	// initialize kubernetes client, if needed
	if shouldSetupKubeClient(cfg) {
		if err := kubernetes.InitClient(); err != nil {
			log.Fatalf("Failed to initialize kubernetes client: %v", err)
			os.Exit(1)
		}
	}

	// start server
	srv, err := server.Start(cfg)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
		os.Exit(1)
	}
	if err := srv.Wait(); err != nil {
		log.Fatalf("Failed to wait server: %v", err)
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
