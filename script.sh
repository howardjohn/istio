istioctl install -y -d manifests/ --set meshConfig.accessLogFile=/dev/stdout
kubectl apply -f samples/addons/grafana.yaml
kubectl apply -f samples/addons/prometheus.yaml
kubectl apply -f samples/addons/jaeger.yaml
kubectl apply -f samples/addons/loki.yaml
kubectl apply -f samples/addons/promtail.yaml

kubectl apply -f ~/istio/latest/samples/bookinfo/platform/kube/bookinfo.yaml
kubectl apply -f ~/istio/latest/samples/bookinfo/networking/bookinfo-gateway.yaml
kubectl apply -f ~/istio/latest/samples/bookinfo/networking/destination-rule-all.yaml

export INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')
export SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')
export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
echo $GATEWAY_URL

curl http://$GATEWAY_URL/productpage
