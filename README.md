```
helm upgrade --install cilium cilium/cilium -n kube-system \
    --set securityContext.privileged=true --set debug.verbose=datapath \
    --set l7Proxy=false --set kubeProxyReplacement=strict \
    --set k8sServiceHost="$(kubectl get endpoints kubernetes -ojson | jq '.subsets[0].addresses[0].ip' -r)" \
    --set k8sServicePort=6443 \
    --set image.repository=gcr.io/howardjohn-istio/cilium/cilium --set image.tag=waypoint-1 --set image.useDigest=false

# Checkout https://github.com/howardjohn/istio/tree/exp/waypoint-gamma-with-waypoint-obj
kubectl apply -f tests/integration/pilot/testdata/gateway-api-crd.yaml
go run ./istioctl/cmd/istioctl install -y -d manifests/ \
    --set profile=minimal \
    --set values.pilot.image=gcr.io/howardjohn-istio/pilot:waypoint-1 \
    --set meshConfig.accessLogFile=/dev/stdout

kubectl apply -f waypoint/example.yaml
kubectl apply -f waypoint/echo.yaml
kubectl annotate namespace default istio.io/use-waypoint=default
kubectl apply -f waypoint/shell.yaml

kubectl exec svc/shell -- curl -s echo
```