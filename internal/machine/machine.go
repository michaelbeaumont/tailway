package machine

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayapi_alpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1beta1"
	"tailscale.com/client/tailscale"
	"tailscale.com/ipn/ipnstate"
)

func FromBuilder(
	logger logr.Logger,
	mgr manager.Manager,
) error {
	var tlc tailscale.LocalClient

	var status *ipnstate.Status
	for {
		var err error
		status, err = tlc.Status(context.Background())
		if err != nil {
			logger.Error(err, "could not get status")
		}
		if status.BackendState == "Running" {
			break
		}
		time.Sleep(1 * time.Second)
	}

	name := strings.TrimSuffix(status.Self.DNSName, ".")

	logger = logger.WithName("machine").WithValues("name", name)

	if err := builder.
		ControllerManagedBy(mgr).
		For(&gatewayapi_alpha.TCPRoute{}).
		Complete(&TCPRouteController{
			Client: mgr.GetClient(),
			TLC:    &tlc,
			Logger: logger.WithValues("resource", "TCPRoute"),
			Name:   name,
		}); err != nil {
		return err
	}

	if err := builder.
		ControllerManagedBy(mgr).
		For(&gatewayapi.Gateway{}).
		Complete(&GatewayController{
			Client: mgr.GetClient(),
			TLC:    &tlc,
			Logger: logger.WithValues("resource", "Gateway"),
		}); err != nil {
		return err
	}

	return nil
}
