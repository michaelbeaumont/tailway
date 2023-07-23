package machine

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	api_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1beta1"
	"tailscale.com/client/tailscale"

	"github.com/michaelbeaumont/tailway/pkg"
)

type GatewayController struct {
	client.Client
	Logger logr.Logger
	TLC    *tailscale.LocalClient
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
		if api_errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	if class.Spec.ControllerName != pkg.ControllerName ||
		!meta.IsStatusConditionTrue(class.Status.Conditions, string(gatewayapi.GatewayClassConditionStatusAccepted)) {
		return reconcile.Result{}, nil
	}

	if err := ctrlr.setStatus(ctx, gateway); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (ctrlr *GatewayController) setStatus(
	ctx context.Context,
	gateway *gatewayapi.Gateway,
) error {
	machineStatus, err := ctrlr.TLC.Status(ctx)
	if err != nil {
		return errors.Wrap(err, "couldn't get tailscale status")
	}

	orig := gateway.DeepCopyObject().(client.Object)

	hostname := gatewayapi.HostnameAddressType
	addrs := []gatewayapi.GatewayAddress{{
		Type:  &hostname,
		Value: strings.TrimSuffix(machineStatus.Self.DNSName, "."),
	}}
	ip := gatewayapi.IPAddressType
	for _, addr := range machineStatus.Self.TailscaleIPs {
		addrs = append(addrs, gatewayapi.GatewayAddress{
			Type:  &ip,
			Value: addr.String(),
		})
	}
	gateway.Status.Addresses = addrs

	meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
		ObservedGeneration: gateway.GetGeneration(),
		Type:               string(gatewayapi.GatewayConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayapi.GatewayReasonAccepted),
	})
	meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
		ObservedGeneration: gateway.GetGeneration(),
		Type:               string(gatewayapi.GatewayConditionProgrammed),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayapi.GatewayConditionProgrammed),
	})

	return ctrlr.Status().Patch(ctx, gateway, client.MergeFrom(orig))
}
