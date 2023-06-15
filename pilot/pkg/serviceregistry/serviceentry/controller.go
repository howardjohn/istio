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

package serviceentry

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/cv2"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

var (
	_   serviceregistry.Instance = &Controller{}
	log                          = istiolog.RegisterScope("serviceentry", "ServiceEntry registry")
)

var (
	prime  = 65011     // Used for secondary hash function.
	maxIPs = 255 * 255 // Maximum possible IPs for address allocation.
)

// instancesKey acts as a key to identify all instances for a given hostname/namespace pair
// This is mostly used as an index
type instancesKey struct {
	hostname  host.Name
	namespace string
}

type octetPair struct {
	thirdOctet  int
	fourthOctet int
}

func makeInstanceKey(i *model.ServiceInstance) instancesKey {
	return instancesKey{i.Service.Hostname, i.Service.Attributes.Namespace}
}

type configType int

const (
	serviceEntryConfigType configType = iota
	workloadEntryConfigType
	podConfigType
)

// configKey unique identifies a config object managed by this registry (ServiceEntry and WorkloadEntry)
type configKey struct {
	kind      configType
	name      string
	namespace string
}

// Controller communicates with ServiceEntry CRDs and monitors for changes.
type Controller struct {
	XdsUpdater model.XDSUpdater

	store     model.ConfigStore
	clusterID cluster.ID

	// This lock is to make multi ops on the below stores. For example, in some case,
	// it requires delete all instances and then update new ones.
	mutex sync.RWMutex

	// To make sure the eds update run in serial to prevent stale ones can override new ones
	// when edsUpdate is called concurrently.
	// If all share one lock, then all the threads can have an obvious performance downgrade.
	edsQueue queue.Instance

	workloadHandlers []func(*model.WorkloadInstance, model.Event)

	// callback function used to get the networkID according to workload ip and labels.
	networkIDCallback func(IP string, labels labels.Instance) network.ID

	// Indicates whether this controller is for workload entries.
	workloadEntryController bool

	model.NoopAmbientIndexes
	model.NetworkGatewaysHandler
	ServicesC cv2.Collection[Service]
	Instances cv2.Collection[Instance]
}

type Option func(*Controller)

func WithClusterID(clusterID cluster.ID) Option {
	return func(o *Controller) {
		o.clusterID = clusterID
	}
}

func WithNetworkIDCb(cb func(endpointIP string, labels labels.Instance) network.ID) Option {
	return func(o *Controller) {
		o.networkIDCallback = cb
	}
}

// NewController creates a new ServiceEntry discovery service.
func NewController(configController model.ConfigStoreController, xdsUpdater model.XDSUpdater,
	options ...Option,
) *Controller {
	s := newController(configController, xdsUpdater, options...)

	WorkloadEntry := cv2.NewConfigStore(gvk.WorkloadEntry, configController)
	WorkloadInstances := cv2.NewCollection[config.Config, model.WorkloadInstance](WorkloadEntry, func(ctx cv2.HandlerContext, i config.Config) *model.WorkloadInstance {
		return s.convertWorkloadEntryToWorkloadInstance(i, s.Cluster())
	})
	ServiceEntries := cv2.NewConfigStore(gvk.ServiceEntry, configController)
	ServiceEntries.Register(func(o cv2.Event[config.Config]) {
		log.Errorf("howardjohn: SE %v %v", o.Event, config.NamespacedName(o.Latest()))
	})
	Services := cv2.NewManyCollection(ServiceEntries, func(ctx cv2.HandlerContext, curr config.Config) []Service {
		cs := convertServices(curr)
		se := curr.Spec.(*networking.ServiceEntry)
		return cv2.Map(cs, func(svc *model.Service) Service {
			return Service{
				Service:    svc,
				Name:       curr.Name,
				EntryPorts: se.Ports,
				Resolution: se.Resolution,
				Selector:   se.WorkloadSelector,
			}
		})
	})
	shard := model.ShardKeyFromRegistry(s)
	NonSelectorInstances := cv2.NewManyCollection(ServiceEntries, func(ctx cv2.HandlerContext, curr config.Config) []Instance {
		cs := convertServices(curr)
		serviceInstances := s.convertServiceEntryToInstances(curr, cs)
		res := make([]Instance, 0, len(serviceInstances))
		for i, s := range serviceInstances {
			res = append(res, Instance{ServiceInstance: s, Index: i})
		}
		log.Errorf("howardjohn: made %v", len(res))
		return res
	})
	SelectorInstances := cv2.NewManyCollection(Services, func(ctx cv2.HandlerContext, se Service) []Instance {
		if se.Selector == nil {
			return nil
		}
		entries := cv2.Fetch(ctx, WorkloadInstances, cv2.FilterNamespace(se.Attributes.Namespace), cv2.FilterLabel(se.Selector.Labels))
		serviceInstances := cv2.FlattenList(cv2.Map(entries, func(wi model.WorkloadInstance) []*model.ServiceInstance {
			if wi.DNSServiceEntryOnly && se.Resolution != networking.ServiceEntry_DNS &&
				se.Resolution != networking.ServiceEntry_DNS_ROUND_ROBIN {
				log.Debugf("skip selecting workload instance %v/%v for DNS service entry %v", wi.Namespace, wi.Name, se.Hostname)
				return nil
			}
			return convertWorkloadInstanceToServiceInstance(&wi, se)
		}))
		res := make([]Instance, 0, len(serviceInstances))
		for i, s := range serviceInstances {
			res = append(res, Instance{ServiceInstance: s, Index: i})
		}
		return res
	})
	Instances := cv2.JoinCollection(NonSelectorInstances, SelectorInstances)

	// TODO: Services is a bit broken. We have Cfg -> Service, but we can have different configs provide the same Host+namespace.
	// What happens in that case? need to check the old code.
	Services.RegisterBatch(func(o []cv2.Event[Service]) {
		configsUpdated := sets.New[model.ConfigKey]()
		changed := sets.New[instancesKey]()
		for _, event := range o {
			for _, svc := range event.Items() {
				configsUpdated.Insert(model.ConfigKey{
					Kind:      kind.ServiceEntry,
					Name:      string(svc.Hostname),
					Namespace: svc.Attributes.Namespace,
				})
				s.XdsUpdater.SvcUpdate(shard, string(svc.Hostname), svc.Attributes.Namespace, model.Event(event.Event))
				changed.Insert(instancesKey{
					hostname:  svc.Hostname,
					namespace: svc.Attributes.Namespace,
				})
			}
		}
		endpoints := s.buildEndpoints(changed)

		full := len(configsUpdated) > 0
		for k, eps := range endpoints {
			_ = eps
			log.Errorf("howardjohn: Service would EDS push %v: %v", full, k.hostname)
			//	if full {
			//		s.XdsUpdater.EDSCacheUpdate(shard, string(k.hostname), k.namespace, eps)
			//	} else {
			//		s.XdsUpdater.EDSUpdate(shard, string(k.hostname), k.namespace, eps)
			//	}
		}
		s.XdsUpdater.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: configsUpdated,
			Reason:         []model.TriggerReason{model.EndpointUpdate},
		})
	})
	Instances.RegisterBatch(func(o []cv2.Event[Instance]) {
		changed := sets.New[instancesKey]()
		configsUpdated := sets.New[model.ConfigKey]()
		proxiesUpdated := sets.New[string]()
		proxiesAdded := sets.New[string]()
		for _, i := range o {
			if i.Event == controllers.EventUpdate {
				if !maps.Equal(i.Old.Endpoint.Labels, i.New.Endpoint.Labels) {
					proxiesUpdated.Insert(i.New.Endpoint.Address)
				}
			} else if i.Event == controllers.EventAdd {
				proxiesAdded.Insert(i.New.Endpoint.Address)
			}
			for _, cfg := range i.Items() {
				if cfg.Service.Resolution == model.DNSLB || cfg.Service.Resolution == model.DNSRoundRobinLB {
					configsUpdated.Insert(model.ConfigKey{
						Kind:      kind.ServiceEntry,
						Name:      string(cfg.Service.Hostname),
						Namespace: cfg.GetNamespace(),
					})
				}
			}
			changed.Insert(instancesKey{
				hostname:  i.Latest().Service.Hostname,
				namespace: i.Latest().GetNamespace(),
			})
		}
		endpoints := s.buildEndpoints(changed)
		shard := model.ShardKeyFromRegistry(s)

		full := len(configsUpdated) > 0
		for ip := range proxiesUpdated {
			s.XdsUpdater.ProxyUpdate(s.Cluster(), ip)
		}
		if !full {
			for ip := range proxiesAdded {
				s.XdsUpdater.ProxyUpdate(s.Cluster(), ip)
			}
		}
		for k, eps := range endpoints {
			log.Errorf("howardjohn: Instance EDS push %v: %v", full, k.hostname)
			if full {
				s.XdsUpdater.EDSCacheUpdate(shard, string(k.hostname), k.namespace, eps)
			} else {
				s.XdsUpdater.EDSUpdate(shard, string(k.hostname), k.namespace, eps)
			}
		}
		if full {
			s.XdsUpdater.ConfigUpdate(&model.PushRequest{
				Full:           true,
				ConfigsUpdated: configsUpdated,
				Reason:         []model.TriggerReason{model.EndpointUpdate},
			})
		}
	})
	s.Instances = Instances
	s.ServicesC = Services
	return s
}

// NewWorkloadEntryController creates a new WorkloadEntry discovery service.
func NewWorkloadEntryController(configController model.ConfigStoreController, xdsUpdater model.XDSUpdater,
	options ...Option,
) *Controller {
	s := newController(configController, xdsUpdater, options...)
	// Disable service entry processing for workload entry controller.
	s.workloadEntryController = true
	for _, o := range options {
		o(s)
	}

	s.ServicesC = cv2.NewStatic[Service](nil).AsCollection()
	s.Instances = cv2.NewStatic[Instance](nil).AsCollection()
	return s
}

type Service struct {
	*model.Service
	// ServiceEntry name. This is odd, but we support the same hostname+namespace pair across entries
	Name       string
	Resolution networking.ServiceEntry_Resolution
	EntryPorts []*networking.ServicePort
	Selector   *networking.WorkloadSelector
}

func (i Service) ResourceName() string {
	return slices.Join("/", i.Service.Attributes.Namespace, i.Service.Hostname.String(), i.Name)
}

type Instance struct {
	*model.ServiceInstance
	Index int
}

func (i Instance) GetNamespace() string {
	return i.Service.Attributes.Namespace
}

func (i Instance) ResourceName() string {
	return slices.Join(
		"/",
		i.Service.Attributes.Name,
		i.Service.Attributes.Namespace,
		i.Service.Hostname.String(),
		i.ServiceInstance.Endpoint.Address,
		strconv.Itoa(i.Index),
	)
}

type Thingy struct {
	cv2.Named
	Services  []*model.Service
	Instances []*model.ServiceInstance
}

func newController(store model.ConfigStoreController, xdsUpdater model.XDSUpdater, options ...Option) *Controller {
	s := &Controller{
		XdsUpdater: xdsUpdater,
		store:      store,
		edsQueue:   queue.NewQueue(time.Second),
	}
	for _, o := range options {
		o(s)
	}
	return s
}

// convertWorkloadEntry convert wle from Config.Spec and populate the metadata labels into it.
func convertWorkloadEntry(cfg config.Config) *networking.WorkloadEntry {
	wle := cfg.Spec.(*networking.WorkloadEntry)
	if wle == nil {
		return nil
	}

	labels := make(map[string]string, len(wle.Labels)+len(cfg.Labels))
	for k, v := range wle.Labels {
		labels[k] = v
	}
	// we will merge labels from metadata with spec, with precedence to the metadata
	for k, v := range cfg.Labels {
		labels[k] = v
	}
	// shallow copy
	copied := &networking.WorkloadEntry{}
	protomarshal.ShallowCopy(copied, wle)
	copied.Labels = labels
	return copied
}

func (s *Controller) Provider() provider.ID {
	return provider.External
}

func (s *Controller) Cluster() cluster.ID {
	return s.clusterID
}

// AppendServiceHandler adds service resource event handler. Service Entries does not use these handlers.
func (s *Controller) AppendServiceHandler(_ model.ServiceHandler) {}

// AppendWorkloadHandler adds instance event handler. Service Entries does not use these handlers.
func (s *Controller) AppendWorkloadHandler(h func(*model.WorkloadInstance, model.Event)) {
	s.workloadHandlers = append(s.workloadHandlers, h)
}

// Run is used by some controllers to execute background jobs after init is done.
func (s *Controller) Run(stopCh <-chan struct{}) {
	s.edsQueue.Run(stopCh)
}

// HasSynced always returns true for SE
func (s *Controller) HasSynced() bool {
	return true
}

func (s *Controller) Services() []*model.Service {
	return cv2.Map(s.ServicesC.List(metav1.NamespaceAll), func(t Service) *model.Service {
		return t.Service
	})
}

// GetService retrieves a service by host name if it exists.
// NOTE: The service entry implementation is used only for tests.
func (s *Controller) GetService(hostname host.Name) *model.Service {
	if s.workloadEntryController {
		return nil
	}
	// TODO(@hzxuzhonghu): only get the specific service instead of converting all the serviceEntries
	services := s.Services()
	for _, service := range services {
		if service.Hostname == hostname {
			return service
		}
	}

	return nil
}

// InstancesByPort retrieves instances for a service on the given ports with labels that
// match any of the supplied labels. All instances match an empty tag list.
func (s *Controller) InstancesByPort(svc *model.Service, port int) []*model.ServiceInstance {
	// TODO: build an index?
	return cv2.MapFilter(s.Instances.List(svc.Attributes.Namespace), func(i Instance) **model.ServiceInstance {
		if i.Service.Hostname != svc.Hostname {
			return nil
		}
		if port != 0 && i.ServicePort.Port != port {
			return nil
		}
		return &i.ServiceInstance
	})
}

// buildEndpoints builds endpoints for the instance keys.
func (s *Controller) buildEndpoints(keys map[instancesKey]struct{}) map[instancesKey][]*model.IstioEndpoint {
	res := make(map[instancesKey][]*model.IstioEndpoint)
	for key := range keys {
		v := cv2.MapFilter(s.Instances.List(key.namespace), func(t Instance) **model.IstioEndpoint {
			if t.Service.Hostname == key.hostname {
				return &t.Endpoint
			}
			return nil
		})
		res[key] = v
	}
	return res
}

// GetProxyServiceInstances lists service instances co-located with a given proxy
// NOTE: The service objects in these instances do not have the auto allocated IP set.
func (s *Controller) GetProxyServiceInstances(node *model.Proxy) []*model.ServiceInstance {
	ips := sets.New(node.IPAddresses...)
	return cv2.MapFilter(s.Instances.List(node.Metadata.Namespace), func(i Instance) **model.ServiceInstance {
		if !ips.Contains(i.Endpoint.Address) {
			return nil
		}
		return &i.ServiceInstance
	})
}

func (s *Controller) GetProxyWorkloadLabels(proxy *model.Proxy) labels.Instance {
	ips := sets.New(proxy.IPAddresses...)
	for _, i := range s.Instances.List(proxy.Metadata.Namespace) {
		if ips.Contains(i.Endpoint.Address) {
			return i.Endpoint.Labels
		}
	}
	return nil
}

func (s *Controller) NetworkGateways() []model.NetworkGateway {
	// TODO implement mesh networks loading logic from kube controller if needed
	return nil
}

func (s *Controller) MCSServices() []model.MCSServiceInfo {
	return nil
}

// Automatically allocates IPs for service entry services WITHOUT an
// address field if the hostname is not a wildcard, or when resolution
// is not NONE. The IPs are allocated from the reserved Class E subnet
// (240.240.0.0/16) that is not reachable outside the pod or reserved
// Benchmarking IP range (2001:2::/48) in RFC5180. When DNS
// capture is enabled, Envoy will resolve the DNS to these IPs. The
// listeners for TCP services will also be set up on these IPs. The
// IPs allocated to a service entry may differ from istiod to istiod
// but it does not matter because these IPs only affect the listener
// IPs on a given proxy managed by a given istiod.
//
// NOTE: If DNS capture is not enabled by the proxy, the automatically
// allocated IP addresses do not take effect.
//
// The current algorithm to allocate IPs is deterministic across all istiods.
func autoAllocateIPs(services []*model.Service) []*model.Service {
	hashedServices := make([]*model.Service, maxIPs)
	hash := fnv.New32a()
	// First iterate through the range of services and determine its position by hash
	// so that we can deterministically allocate an IP.
	// We use "Double Hashning" for collision detection.
	// The hash algorithm is
	// - h1(k) = Sum32 hash of the host name.
	// - Check if we have an empty slot for h1(x) % MAXIPS. Use it if available.
	// - If there is a collision, apply second hash i.e. h2(x) = PRIME - (Key % PRIME)
	//   where PRIME is the max prime number below MAXIPS.
	// - Calculate new hash iteratively till we find an empty slot with (h1(k) + i*h2(k)) % MAXIPS
	for _, svc := range services {
		// we can allocate IPs only if
		// 1. the service has resolution set to static/dns. We cannot allocate
		//   for NONE because we will not know the original DST IP that the application requested.
		// 2. the address is not set (0.0.0.0)
		// 3. the hostname is not a wildcard
		if svc.DefaultAddress == constants.UnspecifiedIP && !svc.Hostname.IsWildCarded() &&
			svc.Resolution != model.Passthrough {
			hash.Write([]byte(makeServiceKey(svc)))
			// First hash is calculated by
			s := hash.Sum32()
			firstHash := s % uint32(maxIPs)
			// Check if there is no service with this hash first. If there is no service
			// at this location - then we can safely assign this position for this service.
			if hashedServices[firstHash] == nil {
				hashedServices[firstHash] = svc
			} else {
				// This means we have a collision. Resolve collision by "DoubleHashing".
				i := uint32(1)
				for {
					secondHash := uint32(prime) - (s % uint32(prime))
					nh := (s + i*secondHash) % uint32(maxIPs)
					if hashedServices[nh] == nil {
						hashedServices[nh] = svc
						break
					}
					i++
				}
			}
			hash.Reset()
		}
	}
	// i is everything from 240.240.0.(j) to 240.240.255.(j)
	// j is everything from 240.240.(i).1 to 240.240.(i).254
	// we can capture this in one integer variable.
	// given X, we can compute i by X/255, and j is X%255
	// To avoid allocating 240.240.(i).255, if X % 255 is 0, increment X.
	// For example, when X=510, the resulting IP would be 240.240.2.0 (invalid)
	// So we bump X to 511, so that the resulting IP is 240.240.2.1
	x := 0
	hnMap := make(map[string]octetPair)
	allocated := 0
	for _, svc := range hashedServices {
		if svc == nil {
			// There is no service in the slot. Just increment x and move forward.
			x++
			if x%255 == 0 {
				x++
			}
			continue
		}
		n := makeServiceKey(svc)
		if v, ok := hnMap[n]; ok {
			log.Debugf("Reuse IP for domain %s", n)
			setAutoAllocatedIPs(svc, v)
		} else {
			x++
			if x%255 == 0 {
				x++
			}
			if allocated >= maxIPs {
				log.Errorf("out of IPs to allocate for service entries. x:= %d, maxips:= %d", x, maxIPs)
				return services
			}
			allocated++
			pair := octetPair{x / 255, x % 255}
			setAutoAllocatedIPs(svc, pair)
			hnMap[n] = pair
		}
	}
	return services
}

func makeServiceKey(svc *model.Service) string {
	return svc.Attributes.Namespace + "/" + svc.Hostname.String()
}

func setAutoAllocatedIPs(svc *model.Service, octets octetPair) {
	a := octets.thirdOctet
	b := octets.fourthOctet
	svc.AutoAllocatedIPv4Address = fmt.Sprintf("240.240.%d.%d", a, b)
	if a == 0 {
		svc.AutoAllocatedIPv6Address = fmt.Sprintf("2001:2::f0f0:%x", b)
	} else {
		svc.AutoAllocatedIPv6Address = fmt.Sprintf("2001:2::f0f0:%x%x", a, b)
	}
}
