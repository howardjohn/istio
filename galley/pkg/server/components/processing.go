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

package components

import (
	"net"
	"sync"

	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/groups"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/status"
	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/galley/pkg/envvar"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/snapshots"
	"istio.io/istio/pkg/mcp/monitoring"
	mcprate "istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
)

// Processing component is the main config processing component that will listen to a config source and publish
// resources through an MCP server, or a dialout connection.
type Processing struct {
	args *settings.Args

	mcpCache *snapshot.Cache

	k kube.Interfaces

	serveWG       sync.WaitGroup
	runtime       *processing.Runtime
	reporter      monitoring.Reporter
	listenerMutex sync.Mutex
	listener      net.Listener
	stopCh        chan struct{}
}

// NewProcessing returns a new processing component.
func NewProcessing(a *settings.Args) *Processing {
	mcpCache := snapshot.New(groups.IndexFunction)
	return &Processing{
		args:     a,
		mcpCache: mcpCache,
	}
}

// Start implements process.Component
func (p *Processing) Start() (err error) {
	var mesh event.Source
	var src event.Source
	var updater snapshotter.StatusUpdater

	if mesh, err = meshcfgNewFS(p.args.MeshConfigFile); err != nil {
		return
	}

	m := schema.MustGet()

	transformProviders := transforms.Providers(m)

	// Disable any unnecessary resources, including resources not in configured snapshots
	var colsInSnapshots collection.Names
	for _, c := range m.AllCollectionsInSnapshots(p.args.Snapshots) {
		colsInSnapshots = append(colsInSnapshots, collection.NewName(c))
	}
	kubeResources := kuberesource.DisableExcludedCollections(m.KubeCollections(), transformProviders,
		colsInSnapshots, p.args.ExcludedResourceKinds, p.args.EnableServiceDiscovery)

	if src, updater, err = p.createSourceAndStatusUpdater(kubeResources); err != nil {
		return
	}

	var distributor snapshotter.Distributor = snapshotter.NewMCPDistributor(p.mcpCache)

	if p.args.EnableConfigAnalysis {
		combinedAnalyzer := analyzers.AllCombined()
		combinedAnalyzer.RemoveSkipped(colsInSnapshots, kubeResources.DisabledCollectionNames(), transformProviders)

		distributor = snapshotter.NewAnalyzingDistributor(snapshotter.AnalyzingDistributorSettings{
			StatusUpdater:     updater,
			Analyzer:          combinedAnalyzer,
			Distributor:       distributor,
			AnalysisSnapshots: p.args.Snapshots,
			TriggerSnapshot:   p.args.TriggerSnapshot,
		})
	}

	processorSettings := processor.Settings{
		Metadata:           m,
		DomainSuffix:       p.args.DomainSuffix,
		Source:             event.CombineSources(mesh, src),
		TransformProviders: transformProviders,
		Distributor:        distributor,
		EnabledSnapshots:   p.args.Snapshots,
	}
	if p.runtime, err = processorInitialize(processorSettings); err != nil {
		return
	}

	p.stopCh = make(chan struct{})

	p.reporter = mcpMetricReporter("galley")

	mcpSourceRateLimiter := mcprate.NewRateLimiter(envvar.MCPSourceReqFreq.Get(), envvar.MCPSourceReqBurstSize.Get())
	options := &source.Options{
		Watcher:            p.mcpCache,
		Reporter:           p.reporter,
		CollectionsOptions: source.CollectionOptionsFromSlice(m.AllCollectionsInSnapshots(snapshots.SnapshotNames())),
		ConnRateLimiter:    mcpSourceRateLimiter,
	}

	// set incremental flag of all collections to true when incremental mcp enabled
	if envvar.EnableIncrementalMCP.Get() {
		for i := range options.CollectionsOptions {
			options.CollectionsOptions[i].Incremental = true
		}
	}

	p.serveWG.Add(1)
	go func() {
		defer p.serveWG.Done()
		p.runtime.Start()
	}()

	return nil
}

func (p *Processing) getKubeInterfaces() (k kube.Interfaces, err error) {
	if p.args.KubeRestConfig != nil {
		return kube.NewInterfaces(p.args.KubeRestConfig), nil
	}
	if p.k == nil {
		p.k, err = newInterfaces(p.args.KubeConfig)
	}
	k = p.k
	return
}

func (p *Processing) createSourceAndStatusUpdater(schemas collection.Schemas) (
	src event.Source, updater snapshotter.StatusUpdater, err error) {

	if p.args.ConfigPath != "" {
		if src, err = fsNew(p.args.ConfigPath, schemas, p.args.WatchConfigFiles); err != nil {
			return
		}
		updater = &snapshotter.InMemoryStatusUpdater{}
	} else {
		var k kube.Interfaces
		if k, err = p.getKubeInterfaces(); err != nil {
			return
		}

		var statusCtl status.Controller
		if p.args.EnableConfigAnalysis {
			statusCtl = status.NewController("validationMessages")
		}

		o := apiserver.Options{
			Client:            k,
			WatchedNamespaces: p.args.WatchedNamespaces,
			ResyncPeriod:      p.args.ResyncPeriod,
			Schemas:           schemas,
			StatusController:  statusCtl,
		}
		s := apiserver.New(o)
		src = s
		updater = s
	}
	return
}

// Stop implements process.Component
func (p *Processing) Stop() {
	if p.stopCh != nil {
		close(p.stopCh)
		p.stopCh = nil
	}

	if p.runtime != nil {
		p.runtime.Stop()
		p.runtime = nil
	}

	if p.reporter != nil {
		_ = p.reporter.Close()
		p.reporter = nil
	}

	p.serveWG.Wait()

	// final attempt to purge buffered logs
	_ = log.Sync()
}
