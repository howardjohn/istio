// +build integ
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

package security

import (
	"context"
	"fmt"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common/response"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

// TestPassThroughFilterChain tests the authN and authZ policy on the pass through filter chain.
func TestPassThroughFilterChain(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.filterchain").
		Run(func(ctx framework.TestContext) {
			ns := apps.Namespace1
			args := map[string]string{
				"Namespace": ns.Name(),
			}
			policies := tmpl.EvaluateAllOrFail(t, args,
				file.AsStringOrFail(t, "testdata/pass-through-filter-chain.yaml.tmpl"))
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), policies...)
			fmt.Println(policies)
			// TODO add partially permisive

			for _, cluster := range ctx.Clusters() {
				a := apps.A.Match(echo.Namespace(ns.Name())).GetOrFail(ctx, echo.InCluster(cluster))
				b := apps.B.Match(echo.Namespace(ns.Name())).GetOrFail(ctx, echo.InCluster(cluster))
				c := apps.C.Match(echo.Namespace(ns.Name())).GetOrFail(ctx, echo.InCluster(cluster))
				d := apps.D.Match(echo.Namespace(ns.Name())).GetOrFail(ctx, echo.InCluster(cluster))
				e := apps.E.Match(echo.Namespace(ns.Name())).GetOrFail(ctx, echo.InCluster(cluster))
				f := apps.F.Match(echo.Namespace(ns.Name())).GetOrFail(ctx, echo.InCluster(cluster))
				cases := []struct {
					target echo.Instance
					port   int
					schema protocol.Instance
					want   bool
				}{
					// For workload a, there is no authN/authZ policy.
					// All requests should success, this is to verify the pass through filter chain and
					// the workload ports are working correctly.
					{
						target: a,
						port:   8085,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: a,
						port:   8086,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: a,
						port:   8087,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: a,
						port:   8088,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: a,
						port:   8089,
						schema: protocol.HTTPS,
						want:   true,
					},

					// For workload b, there is only authZ policy that allows access to port 8085 and 8087.
					// Only request to port 8085, 8087, 8089 should be allowed.
					{
						target: b,
						port:   8085,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: b,
						port:   8086,
						schema: protocol.HTTP,
						want:   false,
					},
					{
						target: b,
						port:   8087,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: b,
						port:   8088,
						schema: protocol.TCP,
						want:   false,
					},
					{
						target: b,
						port:   8089,
						schema: protocol.HTTPS,
						want:   true,
					},
					{
						target: b,
						port:   8084,
						schema: protocol.HTTPS,
						want:   false,
					},

					// For workload c, there is only authN policy that enables mTLS (Strict).
					// The request should be denied because the a is always using plain text.
					{
						target: c,
						port:   8085,
						schema: protocol.HTTP,
						want:   false,
					},
					{
						target: c,
						port:   8086,
						schema: protocol.HTTP,
						want:   false,
					},
					{
						target: c,
						port:   8087,
						schema: protocol.TCP,
						want:   false,
					},
					{
						target: c,
						port:   8088,
						schema: protocol.TCP,
						want:   false,
					},
					{
						target: c,
						port:   8089,
						schema: protocol.HTTPS,
						want:   false,
					},

					// For workload d, there is only authN policy that enables mTLS (Permissive).
					// The request should be allowed because the a is always using plain text.
					{
						target: d,
						port:   8085,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: d,
						port:   8086,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: d,
						port:   8087,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: d,
						port:   8088,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: d,
						port:   8089,
						schema: protocol.HTTPS,
						want:   true,
					},

					// For workload e, there is only authN policy that disables mTLS by default and enables mTLS strict on port 8086 and 8088.
					// The request should be denied on port 8086, 8084, and 8088.
					{
						target: e,
						port:   8085,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: e,
						port:   8086,
						schema: protocol.HTTP,
						want:   false,
					},
					{
						target: e,
						port:   8087,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: e,
						port:   8088,
						schema: protocol.TCP,
						want:   false,
					},
					{
						target: e,
						port:   8089,
						schema: protocol.HTTPS,
						want:   true,
					},
					{
						target: e,
						port:   8084,
						schema: protocol.HTTPS,
						want:   false,
					},

					// For workload f, there is only authN policy that enables mTLS by default and disables mTLS strict on port 8086 and 8088.
					// The request should be denied on port 8085 and 8071.
					{
						target: f,
						port:   8085,
						schema: protocol.HTTP,
						want:   false,
					},
					{
						target: f,
						port:   8086,
						schema: protocol.HTTP,
						want:   true,
					},
					{
						target: f,
						port:   8087,
						schema: protocol.TCP,
						want:   false,
					},
					{
						target: f,
						port:   8088,
						schema: protocol.TCP,
						want:   true,
					},
					{
						target: f,
						port:   8089,
						schema: protocol.HTTPS,
						want:   false,
					},
					{
						target: f,
						port:   8084,
						schema: protocol.HTTPS,
						want:   true,
					},
				}
				ctx.NewSubTest(fmt.Sprintf("In %s", cluster.StableName())).Run(func(ctx framework.TestContext) {
					for _, tc := range cases {
						name := fmt.Sprintf("%s:%d[%t]", tc.target.Config().Service, tc.port, tc.want)
						client := apps.Naked.Match(echo.InCluster(cluster)).GetOrFail(ctx, echo.Namespace(ns.Name()))
						from := getWorkload(client, ctx)
						// The request should be handled by the pass through filter chain.
						host := fmt.Sprintf("%s:%d", getWorkload(tc.target, ctx).Address(), tc.port)
						request := &epb.ForwardEchoRequest{
							Url:     fmt.Sprintf("%s://%s", tc.schema, host),
							Message: "HelloWorld",
							Headers: []*epb.Header{
								{
									Key:   "Host",
									Value: host,
								},
							},
						}
						ctx.NewSubTest(name).Run(func(ctx framework.TestContext) {
							retry.UntilSuccessOrFail(ctx, func() error {
								responses, err := from.ForwardEcho(context.TODO(), request)
								if tc.want {
									if err != nil {
										return fmt.Errorf("want allow but got error: %v", err)
									}
									if len(responses) < 1 {
										return fmt.Errorf("received no responses from request to %s", host)
									}
									if tc.schema == protocol.HTTP && response.StatusCodeOK != responses[0].Code {
										return fmt.Errorf("want status %s but got %s", response.StatusCodeOK, responses[0].Code)
									}
								} else {
									// Check HTTP forbidden response
									if len(responses) >= 1 && response.StatusCodeForbidden == responses[0].Code {
										return nil
									}

									if err == nil {
										return fmt.Errorf("want error but did not get one: %v", responses)
									}
								}
								return nil
							}, echo.DefaultCallRetryOptions()...)
						})
					}
				})

			}
		})
}

func getWorkload(instance echo.Instance, t test.Failer) echo.Workload {
	workloads, err := instance.Workloads()
	if err != nil {
		t.Fatalf(fmt.Sprintf("failed to get Subsets: %v", err))
	}
	if len(workloads) < 1 {
		t.Fatalf("want at least 1 workload but found 0")
	}
	return workloads[0]
}
