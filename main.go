package main

import (
	_ "embed"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/waypoint"
)

func main() {
	// log.EnableKlogWithVerbosity(6)
	c, err := kube.NewDefaultClient()
	fatal(err)
	stop := make(chan struct{})
	ctl := waypoint.NewController(c)
	go ctl.Run(stop)
	go c.RunAndWait(stop)
	cmd.WaitSignal(stop)
}

func fatal(err error) {
	if err != nil {
		panic(err)
	}
}
