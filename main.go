package main

import _ "istio.io/istio/slowinit"
import "time"
//import _ "k8s.io/client-go/kubernetes"
import _ "istio.io/istio/pilot/cmd/pilot-discovery/app"

func init() {
	time.Sleep(time.Second*5)
	print("init done\n")
}

func main() {
	<-time.After(time.Minute*10)
}