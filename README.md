# Tailway

Tailway is an implementation of a very limited subset of Gateway API using
Tailscale.

The idea is to improve on the current k8s-operator by handling TLS termination and
certificate provisioning.

It's mostly a weekend experiment. It can never be a compliant Gateway API
implementation without using some other proxy. In fact, the upstream `LoadBalancer`
controller, once it supports TLS and address tracking, is probably the better
option.

## Installation and usage

Deploy the controller:

```
$ kubectl apply -f manifests/controller.yaml
```

and create a `GatewayClass` pointing to Tailscale oauth credentials:

```
# manifests/config.yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: my-tailnet-oauth
  namespace: tailway-system
stringData:
  client_id: # oauth client_id
  client_secret: # oauth client_secret
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: GatewayClass
metadata:
  name: my-tailnet
  annotations:
    # tags the managed machines should have
    tailway.michaelbeaumont.github.io/tags: tag:k8s
spec:
  controllerName: "tailway.michaelbeaumont.github.io/controller"
  parametersRef:
    kind: Secret
    group: ""
    name: my-tailnet-oauth
    namespace: tailway-system
```

then launch a `Gateway`:

```
---
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: nginx
spec:
  gatewayClassName: my-tailnet
  listeners:
    - port: 443
      name: https
      protocol: TLS
      tls:
        # this is required by the Gateway API webhook but
        # isn't used. Tailscale provisions certs.
        certificateRefs: [name: dummy]
  # you can specify the name of your machine
  # otherwise a default of <name>-<namespace> is used
  addresses:
    - type: Hostname
      value: nginx
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: TCPRoute
metadata:
  name: nginx
spec:
  # only one rule with one backend is supported
  rules:
    - backendRefs:
        - name: nginx
          port: 80
  parentRefs:
    - name: my-tailnet
      kind: nginx
```

The various addresses of the created machine are tracked in the `Gateway` status:

```
  status:
    addresses:
    - type: Hostname
      value: nginx.my-tailnet.ts.net
    - type: IPAddress
      value: 100.124.73.39
    - type: IPAddress
      value: fd7a:225c:a1f0:ab13:4843:cd96:627c:4927
```

## WIP

- [ ] handle conflicts (existing machines, listener conflicts, etc)
- [ ] handle deletion of gateways
- [ ] Dockerfile: why doesn't distroless work?
- [ ] limit RBAC permissions
- [ ] webhook
- [ ] more status/condition setting
- [ ] parametersRef
