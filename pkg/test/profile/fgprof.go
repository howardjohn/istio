// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package profile

import (
	"flag"
	"os"
	"runtime/pprof"

	"github.com/felixge/fgprof"

	"istio.io/istio/pkg/test"
	"istio.io/pkg/log"
)

var fprof string

// init initializes additional profiling flags
// Go comes with some, like -cpuprofile and -memprofile by default, so those are elided.
func init() {
	flag.StringVar(&fprof, "fullprofile", "", "enable full profile. Path will be relative to test directory")
}

// FullProfile runs a "Full" profile (https://github.com/felixge/fgprof). This differs from standard
// CPU profile, as it includes both IO blocking and CPU usage in one profile, giving a full view of
// the application.
func FullProfile(t test.Failer) {
	if fprof == "" {
		return
	}
	f, err := os.Create(fprof)
	if err != nil {
		t.Fatalf("%v", err)
	}
	stop := fgprof.Start(f, fgprof.FormatPprof)

	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func FullProfileBinary(fprof string) func() {
	if fprof == "" {
		return func() {}
	}
	f, err := os.Create(fprof)
	if err != nil {
		log.Errorf("failed to create profile path")
		return func() {}
	}
	stop := fgprof.Start(f, fgprof.FormatPprof)

	return func() {
		if err := stop(); err != nil {
			log.Errorf("failed to stop profile: %v", err)
		}
		if err := f.Close(); err != nil {
			log.Errorf("failed to close profile: %v", err)
		}
	}
}

func CPUProfileBinary(prof string) func() {
	if prof == "" {
		return func() {}
	}
	f, err := os.Create(prof)
	if err != nil {
		log.Errorf("failed to create profile path: %v", err)
		return func() {}
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		log.Errorf("failed to start profile: %v", err)
		return func() {}
	}

	return func() {
		pprof.StopCPUProfile()
		if err := f.Close(); err != nil {
			log.Errorf("failed to close profile: %v", err)
			return
		}
	}
}
