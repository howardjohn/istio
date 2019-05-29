package v2_test

import (
	"log"
	"os"
	"strconv"
	"testing"
	"time"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/features/pilot"
	"istio.io/istio/tests/util"
)

func TestADSThrottle(t *testing.T) {
	os.Setenv(pilot.PushThrottle.Name, "1")
	os.Setenv(pilot.PushBurst.Name, "2")
	server, tearDown := initLocalPilotTestEnv(t)
	defer tearDown()
	adsc, err := adsc.Dial(util.MockPilotGrpcAddr, "", &adsc.Config{
		IP: testIP(uint32(0x0a100000)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer adsc.Close()
	adsc.Watch()
	_, err = adsc.Wait("rds", 10*time.Second)
	time.Sleep(time.Second * 5)
	for j := 0; j < 50; j++ {
		if false {
			// This will be throttled - we want to trigger a single push
			//server.EnvoyXdsServer.MemRegistry.SetEndpoints(edsIncSvc,
			//	newEndpointWithAccount("127.0.0.2", "hello-sa", "v1"))
			updates := map[string]struct{}{
				edsIncSvc: {},
			}
			server.EnvoyXdsServer.AdsPushAll(strconv.Itoa(j), server.EnvoyXdsServer.Env.PushContext, false, updates)
		} else {
			v2.AdsPushAll(server.EnvoyXdsServer)
		}
		log.Println("Push done ", j)
	}
}