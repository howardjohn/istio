k3d cluster create k3s --no-lb --no-rollback --k3s-arg "--disable=coredns@server:*" --k3s-arg "--disable=servicelb@server:*" --k3s-arg "--disable=traefik@server:*" --k3s-arg "--disable=local-storage@server:*" --k3s-arg "--disable=metrics-server@server:*" --k3s-arg "--disable-cloud-controller@server:*" --k3s-arg "--disable-kube-proxy@server:*" --k3s-arg "--disable-network-policy@server:*" --k3s-arg "--disable-helm-controller@server:*" --agents=2

Get the IP, replace in IOP

ikl install -d manifests/ -y -f iop.yaml

Run twice, since we need to get the Istiod pod (hack).
