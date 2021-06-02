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

package gateway

import (
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1alpha1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
)

func createRouteStatus(gateways []string, obj config.Config, current []k8s.RouteGatewayStatus, routeErr *ConfigError) []k8s.RouteGatewayStatus {
	setGateways := map[localNamedReference]struct{}{}
	for _, gw := range gateways {
		ref := localNamedReference{}
		if gw == constants.IstioMeshGateway {
			ref.Name = experimentalMeshGatewayName
			// TODO this is not namespaced but a namespace is required
			ref.Namespace = "default"
		} else {
			s := strings.Split(gw, "/")
			ref.Name = s[1]
			ref.Namespace = s[0]
		}
		setGateways[ref] = struct{}{}
	}
	gws := make([]k8s.RouteGatewayStatus, 0, len(gateways))
	// Fill in all of the gateways that are already present but not owned by use
	for _, r := range current {
		if r.GatewayRef.Controller == nil {
			// Controller not set. This may be our own resource due to using old CRDs that prune the controller field
			_, f := setGateways[localNamedReference{r.GatewayRef.Name, r.GatewayRef.Namespace}]
			if !f {
				// We are not going to set this gateway ref, so we should keep it, it may be owned by some other controller
				gws = append(gws, r)
			}
			// Otherwise we are going to overwrite it with our own status. This could technically overwrite another controller,
			// but there isn't much we can do here. If we appended a status, we would end up infinitely writing our own
			// status
		} else if *r.GatewayRef.Controller != ControllerName {
			// We don't own this status, so keep it around
			gws = append(gws, r)
		}
	}
	// Now we fill in all of the ones we do own
	for _, gw := range gateways {
		ref := k8s.RouteStatusGatewayReference{
			Controller: StrPointer(ControllerName),
		}
		if gw == constants.IstioMeshGateway {
			ref.Name = experimentalMeshGatewayName
			// TODO this is not namespaced but a namespace is required
			ref.Namespace = "default"
		} else {
			s := strings.Split(gw, "/")
			ref.Name = s[1] // TODO name here is the internal name, need to use the external name
			ref.Namespace = s[0]
		}
		var condition metav1.Condition
		if routeErr != nil {
			condition = metav1.Condition{
				Type:               string(k8s.ConditionRouteAdmitted),
				Status:             kstatus.StatusFalse,
				ObservedGeneration: obj.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             routeErr.Reason,
				Message:            routeErr.Message,
			}
		} else {
			condition = metav1.Condition{
				Type:               string(k8s.ConditionRouteAdmitted),
				Status:             kstatus.StatusTrue,
				ObservedGeneration: obj.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             "RouteAdmitted",
				Message:            "Route was valid",
			}
		}
		gws = append(gws, k8s.RouteGatewayStatus{
			GatewayRef: ref,
			Conditions: []metav1.Condition{condition},
		})
	}
	return gws
}

type ConfigErrorReason = string

const (
	// InvalidDestination indicates an issue with the destination
	InvalidDestination ConfigErrorReason = "InvalidDestination"
	// InvalidFilter indicates an issue with the filters
	InvalidFilter ConfigErrorReason = "InvalidFilter"
	// InvalidTLS indicates an issue with TLS settings
	InvalidTLS ConfigErrorReason = "InvalidTLS"
	// InvalidConfiguration indicates a generic error for all other invalid configurations
	InvalidConfiguration ConfigErrorReason = "InvalidConfiguration"
)

// ConfigError represents an invalid configuration that will be reported back to the user.
type ConfigError struct {
	Reason  ConfigErrorReason
	Message string
}

type condition struct {
	// reason defines the reason to report on success. Ignored if error is set
	reason string
	// message defines the message to report on success. Ignored if error is set
	message string
	// status defines the status to report on success. The inverse will be set if error is set
	// If not set, will default to StatusTrue
	status metav1.ConditionStatus
	// error defines an error state; the reason and message will be replaced with that of the error and
	// the status inverted
	error *ConfigError
}

// setConditions sets the existingConditions with the new conditions
func setConditions(generation int64, existingConditions []metav1.Condition, conditions map[string]*condition) []metav1.Condition {
	// Sort keys for deterministic ordering
	condKeys := make([]string, 0, len(conditions))
	for k := range conditions {
		condKeys = append(condKeys, k)
	}
	sort.Strings(condKeys)
	for _, k := range condKeys {
		cond := conditions[k]
		if cond.error != nil {
			existingConditions = kstatus.ConditionallyUpdateCondition(existingConditions, metav1.Condition{
				Type:               k,
				Status:             kstatus.InvertStatus(cond.status),
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             cond.error.Reason,
				Message:            cond.error.Message,
			})
		} else {
			status := cond.status
			if status == "" {
				status = kstatus.StatusTrue
			}
			existingConditions = kstatus.ConditionallyUpdateCondition(existingConditions, metav1.Condition{
				Type:               k,
				Status:             status,
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             cond.reason,
				Message:            cond.message,
			})
		}
	}
	return existingConditions
}

func reportGatewayCondition(obj config.Config, conditions map[string]*condition) {
	obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
		gs := s.(*k8s.GatewayStatus)
		// TODO implement addresses
		gs.Addresses = []k8s.GatewayAddress{}
		gs.Conditions = setConditions(obj.Generation, gs.Conditions, conditions)
		return gs
	})
}

func reportListenerCondition(index int, l k8s.Listener, obj config.Config, conditions map[string]*condition) {
	obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
		gs := s.(*k8s.GatewayStatus)
		if index >= len(gs.Listeners) {
			gs.Listeners = append(gs.Listeners, k8s.ListenerStatus{})
		}
		cond := gs.Listeners[index].Conditions
		gs.Listeners[index] = k8s.ListenerStatus{
			Port:       l.Port,
			Protocol:   l.Protocol,
			Hostname:   l.Hostname,
			Conditions: setConditions(obj.Generation, cond, conditions),
		}
		return gs
	})
}

func reportBackendPolicyCondition(obj config.Config, conditions map[string]*condition) {
	obj.Status.(*kstatus.WrappedStatus).Mutate(func(s config.Status) config.Status {
		bs := s.(*k8s.BackendPolicyStatus)
		bs.Conditions = setConditions(obj.Generation, bs.Conditions, conditions)
		return bs
	})
}
