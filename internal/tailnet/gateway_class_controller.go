package tailnet

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/michaelbeaumont/tailway/pkg"
	"golang.org/x/oauth2/clientcredentials"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1beta1"
	"tailscale.com/client/tailscale"
)

type GatewayClassController struct {
	client.Client
	Logger    logr.Logger
	tsClients *TSClients
}

// gatewayClassField is needed for GatewayClassReconciler
const gatewayClassField = ".metadata.gatewayClass"

func gatewayClassNameIndexer(logger logr.Logger) func(client.Object) []string {
	logger = logger.WithName("gatewayClassNameIndexer")

	return func(obj client.Object) []string {
		gateway, ok := obj.(*gatewayapi.Gateway)
		if !ok {
			logger.Error(nil, "could not convert to Gateway", "object", obj)
			return []string{}
		}

		return []string{string(gateway.Spec.GatewayClassName)}
	}
}

func gatewaysForClass(logger logr.Logger, cl client.Client) handler.MapFunc {
	logger = logger.WithName("gatewaysForClass")
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		class, ok := obj.(*gatewayapi.GatewayClass)
		if !ok {
			logger.Error(nil, "unexpected error converting to be mapped %T object to GatewayClass", obj)
			return nil
		}

		gateways := &gatewayapi.GatewayList{}
		if err := cl.List(
			ctx, gateways, client.MatchingFields{gatewayClassField: class.Name},
		); err != nil {
			logger.Error(err, "unexpected error listing Gateways")
			return nil
		}

		var requests []reconcile.Request
		for i := range gateways.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&gateways.Items[i]),
			})
		}

		return requests
	}
}

func (ctrlr *GatewayClassController) clientFromSecret(ctx context.Context, name types.NamespacedName) (*tailscale.Client, bool, error) {
	secret := v1.Secret{}
	if err := ctrlr.Get(ctx, name, &secret); err != nil {
		if errors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	oauthCredentials := clientcredentials.Config{
		ClientID:     string(secret.Data["client_id"]),
		ClientSecret: string(secret.Data["client_secret"]),
		TokenURL:     tokenURL,
		Scopes:       []string{"devices"},
	}
	ts := tailscale.NewClient("-", nil)
	ts.HTTPClient = oauthCredentials.Client(context.Background())

	return ts, true, nil
}

func (ctrlr *GatewayClassController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	gatewayClass := &gatewayapi.GatewayClass{}
	err := ctrlr.Get(ctx, req.NamespacedName, gatewayClass)
	if err != nil {
		return reconcile.Result{}, err
	}

	if gatewayClass.Spec.ControllerName != pkg.ControllerName {
		return reconcile.Result{}, nil
	}

	accepted := metav1.Condition{
		ObservedGeneration: gatewayClass.GetGeneration(),
		Type:               string(gatewayapi.GatewayClassConditionStatusAccepted),
	}
	orig := gatewayClass.DeepCopyObject().(client.Object)
	ref := gatewayClass.Spec.ParametersRef
	switch {
	case ref != nil &&
		ref.Kind == "Secret" &&
		ref.Group == "" &&
		ref.Namespace != nil &&
		*ref.Namespace == "tailway-system":

		client, found, err := ctrlr.clientFromSecret(
			ctx,
			types.NamespacedName{Name: ref.Name, Namespace: string(*ref.Namespace)},
		)
		if err != nil {
			return reconcile.Result{}, err
		}
		if !found {
			accepted.Status = metav1.ConditionFalse
			accepted.Reason = string(gatewayapi.GatewayClassReasonInvalidParameters)
			accepted.Message = "ParametersRef refers to a nonexistent Secret"
		} else {
			accepted.Status = metav1.ConditionTrue
			accepted.Reason = string(gatewayapi.GatewayClassReasonAccepted)

			ctrlr.tsClients.Lock()
			ctrlr.tsClients.clients[gatewayClass.Name] = client
			ctrlr.tsClients.Unlock()
		}
	default:
		accepted.Status = metav1.ConditionFalse
		accepted.Reason = string(gatewayapi.GatewayClassReasonInvalidParameters)
		accepted.Message = "ParametersRef must be a Secret in tailway's namespace"
	}

	meta.SetStatusCondition(&gatewayClass.Status.Conditions, accepted)
	if err := ctrlr.Status().Patch(ctx, gatewayClass, client.MergeFrom(orig)); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
