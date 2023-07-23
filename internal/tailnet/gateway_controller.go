package tailnet

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/michaelbeaumont/tailway/pkg"
)

type GatewayController struct {
	client.Client
	Logger    logr.Logger
	tsClients *TSClients
}

func (ctrlr *GatewayController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	gateway := &gatewayapi.Gateway{}
	err := ctrlr.Get(ctx, req.NamespacedName, gateway)
	if err != nil {
		return reconcile.Result{}, err
	}

	ctrlr.Logger.Info("checking GatewayClass of parent Gateway", "name", gateway.Spec.GatewayClassName)
	class := &gatewayapi.GatewayClass{}
	if err := ctrlr.Get(ctx, types.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}, class); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	if class.Spec.ControllerName != pkg.ControllerName ||
		!meta.IsStatusConditionTrue(class.Status.Conditions, string(gatewayapi.GatewayClassConditionStatusAccepted)) {
		return reconcile.Result{}, nil
	}

	hostname := fmt.Sprintf("%s-%s", gateway.Name, gateway.Namespace)
	for _, address := range gateway.Spec.Addresses {
		if *address.Type == gatewayapi.HostnameAddressType {
			hostname = address.Value
		}
	}

	ctrlr.Logger.Info("creating node", "name", hostname)

	if err := ctrlr.handleSecret(ctx, gateway, class, hostname); err != nil {
		return reconcile.Result{}, err
	}

	if err := ctrlr.handleDeployment(ctx, gateway, hostname); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
