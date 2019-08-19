package main

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	"io/ioutil"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"os"
)

func main() {
	fname := os.Args[1]
	f, err := os.Open(fname)
	if err != nil {
		panic(err)
	}

	data, err := ioutil.ReadAll(f)
	if err != nil {
		panic(err)
	}

	cfg, _, err := crd.ParseInputs(string(data))
	if err != nil {
		panic(err)
	}

	if len(cfg) != 1 {
		panic(fmt.Errorf("got %v configs, expected 1", cfg))
	}

	bytes, err := proto.Marshal(cfg[0].Spec)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(bytes))
}
