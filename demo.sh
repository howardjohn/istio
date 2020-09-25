# We have a standard Istio installation, with CNI enabled
kubectl get pods -n istio-system

# Injection is enabled
kubectl get namespaces --show-labels
kubectl get pods

# Start up a couple pod
kubectl apply -f ~/kube/shell/shell.yaml
kubectl apply -f ~/kube/apps/echo.yaml

# Send some requests
kubectl exec -it ... -- bash
# Send "curl echo"

# Prove we have mTLS
kubectl apply -f ~/kube/istio/strict.yaml

# Istioctl works as expected
istioctl proxy-config secret <POD>

# Show restart
kubectl rollout restart deployment shell

# Show upgrade
# In one tab:
kubectl exec -it <SHELL> -- bash -c 'while :; do curl echo; sleep 1; done;'

./upgrade.sh echo-proxy-pod | kubectl apply -f -
kubectl get pods
kubectl logs -f <both pods>