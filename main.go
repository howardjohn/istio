package main

import (
	"context"
	"flag"
	"runtime"
	"strconv"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/test/profile"
	"istio.io/pkg/log"
)

func main() {
	flag.Parse()
	defer profile.HeapProfile()
	defer profile.CPUProfile()()
	c := model.NewXdsCache()
	ctx, cancel := context.WithCancel(context.Background())
	go cmd.WaitSignalFunc(cancel)
	wait := func(t time.Duration) bool {
		select {
		case <-time.After(t):
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		for {
			if !wait(time.Second) {
				break
			}
			m := &runtime.MemStats{}
			runtime.ReadMemStats(m)
			log.WithLabels(
				"alloc", util.ByteCount(int(m.Alloc)),
				"total alloc", util.ByteCount(int(m.TotalAlloc)),
				"size", len(c.Keys()),
			).Infof("memory")
		}
	}()
	iter := 0
	zeroTime := time.Time{}
	res := &discovery.Resource{Name: "test"}
	for {
		iter++
		if !wait(time.Millisecond) {
			break
		}
		req := &model.PushRequest{Start: zeroTime.Add(time.Duration(iter))}
		c.Add(makeCacheKey(iter), req, res)
	}
}

func makeCacheKey(n int) model.XdsCacheEntry {
	ns := strconv.Itoa(n)

	// 100 services
	services := make([]*model.Service, 0, 100)
	// 100 destinationrules
	drs := make([]*model.ConsolidatedDestRule, 0, 100)
	for i := 0; i < 100; i++ {
		index := strconv.Itoa(i)
		services = append(services, &model.Service{
			Hostname:   host.Name(ns + "some" + index + ".example.com"),
			Attributes: model.ServiceAttributes{Namespace: "test" + index},
		})
		drs = append(drs, model.ConvertConsolidatedDestRule(&config.Config{Meta: config.Meta{Name: index, Namespace: index}}))
	}

	key := &route.Cache{
		RouteName:        "something",
		ClusterID:        "my-cluster",
		DNSDomain:        "some.domain.example.com",
		DNSCapture:       true,
		DNSAutoAllocate:  false,
		ListenerPort:     1234,
		Services:         services,
		DestinationRules: drs,
		EnvoyFilterKeys:  []string{ns + "1/a", ns + "2/b", ns + "3/c"},
	}
	return key
}
