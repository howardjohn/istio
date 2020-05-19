# Remote control plane

Example remote control plane setup. Workloads connect to a gateway exposed under istiod.howardjohn-mc.qualistio.org, which has a real LetsEncrypt certificate.

## Setup - Control Plane

```
kubectl apply --validate=false -f https://github.com/jetstack/cert-manager/releases/download/v0.15.0/cert-manager.yaml
kubectl apply -f controlplane.yaml
```

The control plane setup deploys a standard out-of-the-box Istio install.

We then install cert-manager and provision an ACME cert for istiod.

Next we set up routing rules for various Istiod functionalities.

NOTE: in a real setup we would also need to add the remote cluster secrets. This would allow us to read the configs, and properly authenticate clients. This is intended just as a proof of concept.

Note that as a result of using a proper ACME cert, the control plane only needs to read the remote cluster - we do not need to patch webhooks nor write the root cert configmap.

## Setup - Workload

```
kubectl apply -f $GOPATH/src/istio.io/istio/manifests/charts/base/crds/crd-all.gen.yaml
kubectl apply -f workload.yaml
```

This deploys **only**:
* the CRDs for Istio (technically not required if you want to do all config in the remote cluster)
* validation webhook
* injection webhook


Validate the validations works (should reject):

```
cat <<EOF | kubectl apply -f -
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: invalid-gateway
spec:
  selector:
    istio: ingressgateway
EOF
```

Deploy a workload with annotations:

```yaml
annotations:
  proxy.istio.io/config: |
    discoveryAddress: istiod.howardjohn-mc.qualistio.org:443
  sidecar.istio.io/proxyImage: gcr.io/howardjohn-istio/proxyv2:full-remote
```

The proxy image is from a fork with some minor modifications: `howardjohn/istio:istio/full-remote`.

The discovery address can be set in the centralized mesh config, but we need a way to ensure the pods in the control plane cluster (such as the ingress) do not get this config. For now, the annotation shows this is possible.
