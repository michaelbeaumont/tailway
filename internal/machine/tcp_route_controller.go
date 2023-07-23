package machine

import (
	"context"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapi_alpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1beta1"
	"tailscale.com/client/tailscale"
	"tailscale.com/ipn"

	"github.com/michaelbeaumont/tailway/pkg"
)

type TCPRouteController struct {
	client.Client
	Logger logr.Logger
	Name   string
	TLC    *tailscale.LocalClient
}

type portProtocol struct {
	port     gatewayapi.PortNumber
	protocol gatewayapi.ProtocolType
}

func (ctrlr *TCPRouteController) relevantGatewayListeners(
	ctx context.Context,
	routeNamespace string,
	parentRef gatewayapi.ParentReference,
) ([]portProtocol, error) {
	ctrlr.Logger.V(1).Info("checking ParentRef", "kind", *parentRef.Kind, "group", *parentRef.Group)
	if string(*parentRef.Kind) != "Gateway" || string(*parentRef.Group) != gatewayapi.GroupVersion.Group {
		return nil, nil
	}

	parentNamespace := routeNamespace
	if parentRef.Namespace != nil {
		parentNamespace = string(*parentRef.Namespace)
	}
	ctrlr.Logger.V(1).Info("checking Gateway parent", "name", parentRef.Name, "namespace", parentNamespace)
	gateway := &gatewayapi.Gateway{}
	if err := ctrlr.Get(ctx, types.NamespacedName{Name: string(parentRef.Name), Namespace: parentNamespace}, gateway); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	ctrlr.Logger.V(1).Info("checking GatewayClass of parent Gateway", "name", gateway.Spec.GatewayClassName)
	class := &gatewayapi.GatewayClass{}
	if err := ctrlr.Get(ctx, types.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}, class); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	if class.Spec.ControllerName != pkg.ControllerName {
		return nil, nil
	}

	ctrlr.Logger.V(1).Info("checking addresses of parent tailway Gateway", "addresses", gateway.Spec.Addresses)
	var foundName bool
	for _, address := range gateway.Spec.Addresses {
		if *address.Type == gatewayapi.HostnameAddressType && strings.HasPrefix(ctrlr.Name, address.Value+".") {
			foundName = true
		}
	}

	if !foundName {
		return nil, nil
	}

	var gatewayPortProtocols []portProtocol

	for _, listener := range gateway.Spec.Listeners {
		gatewayPortProtocols = append(
			gatewayPortProtocols,
			portProtocol{port: listener.Port, protocol: listener.Protocol},
		)
	}

	return gatewayPortProtocols, nil
}

func (ctrlr *TCPRouteController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	route := &gatewayapi_alpha.TCPRoute{}
	err := ctrlr.Get(ctx, req.NamespacedName, route)
	if err != nil {
		return reconcile.Result{}, err
	}

	var gatewayPortProtocols []portProtocol

	var parentRefs []gatewayapi_alpha.ParentReference

	for _, parentRef := range route.Spec.ParentRefs {
		listeners, err := ctrlr.relevantGatewayListeners(ctx, route.Namespace, parentRef)
		if err != nil {
			return reconcile.Result{}, err
		}

		gatewayPortProtocols = append(
			gatewayPortProtocols,
			listeners...,
		)

		if len(listeners) > 0 {
			parentRefs = append(parentRefs, parentRef)
		}
	}

	if len(gatewayPortProtocols) == 0 {
		return reconcile.Result{}, nil
	}

	ctrlr.Logger.Info("reconciling", "HTTPRoute", req.NamespacedName, "portProtocols", gatewayPortProtocols)

	if err := ctrlr.servePortProtocols(ctx, *route, gatewayPortProtocols); err != nil {
		return reconcile.Result{}, err
	}

	if err := ctrlr.setStatus(ctx, route, parentRefs); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (ctrlr *TCPRouteController) servePortProtocols(
	ctx context.Context,
	route gatewayapi_alpha.TCPRoute,
	gatewayPortProtocols []portProtocol,
) error {
	serveConfig, err := ctrlr.TLC.GetServeConfig(ctx)
	if err != nil {
		return err
	}

	if serveConfig == nil {
		serveConfig = &ipn.ServeConfig{}
	}
	if serveConfig.TCP == nil {
		serveConfig.TCP = map[uint16]*ipn.TCPPortHandler{}
	}

	backend := route.Spec.Rules[0].BackendRefs[0].BackendObjectReference
	namespace := route.Namespace
	if backend.Namespace != nil {
		namespace = string(*backend.Namespace)
	}

	svc := v1.Service{}
	if err := ctrlr.Get(
		ctx,
		types.NamespacedName{Name: string(backend.Name), Namespace: namespace},
		&svc,
	); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	tcpForward := net.JoinHostPort(svc.Spec.ClusterIP, strconv.Itoa(int(*backend.Port)))

	for _, portProtocol := range gatewayPortProtocols {
		terminateTLS := ""
		if portProtocol.protocol == gatewayapi.TLSProtocolType {
			terminateTLS = ctrlr.Name
		}
		handler := &ipn.TCPPortHandler{
			TCPForward:   tcpForward,
			TerminateTLS: terminateTLS,
		}
		serveConfig.TCP[uint16(portProtocol.port)] = handler

		ctrlr.Logger.Info("adding handler", "port", portProtocol.port, "handler", handler)
	}

	return ctrlr.TLC.SetServeConfig(ctx, serveConfig)
}

func (ctrlr *TCPRouteController) setStatus(
	ctx context.Context,
	route *gatewayapi_alpha.TCPRoute,
	refs []gatewayapi_alpha.ParentReference,
) error {
	orig := route.DeepCopyObject().(client.Object)

	existingStatuses := map[int]struct{}{}

	for i, parentStatus := range route.Status.Parents {
		if parentStatus.ControllerName == pkg.ControllerName {
			existingStatuses[i] = struct{}{}
		}
	}

	for _, ref := range refs {
		condition := metav1.Condition{
			ObservedGeneration: route.GetGeneration(),
			Type:               string(gatewayapi_alpha.RouteConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayapi_alpha.RouteReasonAccepted),
		}

		var existingStatusEntry *int

		for i := range existingStatuses {
			j := i
			if reflect.DeepEqual(route.Status.Parents[j].ParentRef, ref) {
				existingStatusEntry = &j
				delete(existingStatuses, j)
			}
		}

		if existingStatusEntry == nil {
			previousStatus := gatewayapi_alpha.RouteParentStatus{
				ParentRef:      ref,
				ControllerName: pkg.ControllerName,
				Conditions:     []metav1.Condition{},
			}
			route.Status.Parents = append(route.Status.Parents, previousStatus)
			entry := len(route.Status.Parents) - 1
			existingStatusEntry = &entry
			ctrlr.Logger.Info("added status", "status", route.Status)
		}

		meta.SetStatusCondition(&route.Status.Parents[*existingStatusEntry].Conditions, condition)
	}

	return ctrlr.Status().Patch(ctx, route, client.MergeFrom(orig))
}
