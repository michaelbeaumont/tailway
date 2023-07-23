package tailnet

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1beta1"
	"tailscale.com/client/tailscale"
)

const tokenURL = "https://login.tailscale.com/api/v2/oauth/token"
const fqdnLabel = "tailway.michaelbeaumont.github.io/node-fqdn"
const tagsAnnotation = "tailway.michaelbeaumont.github.io/tags"

const clientIDFile = "/oauth/client_id"
const clientSecretFile = "/oauth/client_secret"

type TSClients struct {
	clients map[string]*tailscale.Client
	sync.Mutex
}

func FromBuilder(logger logr.Logger, mgr manager.Manager) error {
	tailscale.I_Acknowledge_This_API_Is_Unstable = true

	logger = logger.WithName("tailnet")

	clients := TSClients{
		clients: map[string]*tailscale.Client{},
	}

	if err := builder.
		ControllerManagedBy(mgr).
		For(&gatewayapi.GatewayClass{}).
		Complete(&GatewayClassController{
			Client:    mgr.GetClient(),
			Logger:    logger.WithValues("resource", "GatewayClass"),
			tsClients: &clients,
		}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(), &gatewayapi.Gateway{}, gatewayClassField, gatewayClassNameIndexer(logger),
	); err != nil {
		return err
	}

	if err := builder.
		ControllerManagedBy(mgr).
		For(&gatewayapi.Gateway{}).
		Watches(
			&gatewayapi.GatewayClass{},
			handler.EnqueueRequestsFromMapFunc(gatewaysForClass(logger, mgr.GetClient())),
		).
		Complete(&GatewayController{
			Client:    mgr.GetClient(),
			Logger:    logger.WithValues("resource", "Gateway"),
			tsClients: &clients,
		}); err != nil {
		return err
	}

	return nil
}
