package main

import (
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	gatewayapi_alpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/michaelbeaumont/tailway/internal/machine"
	"github.com/michaelbeaumont/tailway/internal/tailnet"
)

func main() {
	logf.SetLogger(zap.New())

	var logger = logf.Log.WithName("tailway")

	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{})
	if err != nil {
		logger.Error(err, "could not create manager")
		os.Exit(1)
	}

	gatewayapi.Install(mgr.GetScheme())
	gatewayapi_alpha.Install(mgr.GetScheme())

	if len(os.Args) != 2 {
		logger.Error(nil, "expected either 'machine' or 'tailnet' as first argument")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "machine":
		if err := machine.FromBuilder(logger, mgr); err != nil {
			logger.Error(err, "could not create machine controller")
			os.Exit(1)
		}
		logger.Info("Starting machine")
	case "tailnet":
		if err := tailnet.FromBuilder(logger, mgr); err != nil {
			logger.Error(err, "could not create tailnet controller")
			os.Exit(1)
		}
		logger.Info("Starting tailnet")
	default:
		logger.Error(nil, "expected either 'machine' or 'tailnet' as first argument")
		os.Exit(1)
	}

	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		logger.Error(err, "could not start manager")
		os.Exit(1)
	}
}
