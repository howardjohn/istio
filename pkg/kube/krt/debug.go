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

package krt

import (
	"encoding/json"
	"fmt"
	"istio.io/istio/pkg/util/sets"
	"strings"
	"sync"
)

// DebugHandler allows attaching a variety of collections to it and then dumping them
type DebugHandler struct {
	debugCollections []DebugCollection
	mu               sync.RWMutex
}

func (p *DebugHandler) MarshalJSON() ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return json.Marshal(p.debugCollections)
}

type Node struct {
	Id   int
	Type int
	Name string
}

func (n Node) String() string { return fmt.Sprintf("node%d", n.Id) }
func (n Node) Descriptor() string {
	switch n.Type {
	case 1:
		return fmt.Sprintf("%s[[%q]]", n.String(), n.Name)
	default:
		return fmt.Sprintf("%s[%q]", n.String(), n.Name)
	}
}

func (p *DebugHandler) Mermaid() ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	id := 0
	primaryDeps := map[Node]sets.Set[Node]{}
	secondaryDeps := map[Node]sets.Set[Node]{}
	nodes := map[string]Node{}
	rawNode := func(s string, typ int) Node {
		if v, f := nodes[s]; f {
			return v
		}
		id++
		n := Node{Id: id, Type: typ, Name: s}
		nodes[s] = n
		return n
	}
	node := func(s string) Node {
		return rawNode(s, 0)
	}
	manyCollectionNode := func(s string) Node {
		return rawNode(s, 1)
	}
	for _, c := range p.debugCollections {
		d := c.dump()
		if d.InputCollection == "" {
			continue
		}
		this := manyCollectionNode(c.name)
		primary := node(d.InputCollection)
		sets.InsertOrNew(primaryDeps, this, primary)
		for _, i := range d.Inputs {
			for _, dep := range i.Dependencies {
				sets.InsertOrNew(secondaryDeps, this, node(dep))
			}
		}
	}
	sb := &strings.Builder{}
	fmt.Fprintf(sb, "flowchart LR\n")
	for n, np := range primaryDeps {
		for npp := range np {
			fmt.Fprintf(sb, "  %s-->%s\n", n, npp)
		}
	}
	for n, np := range secondaryDeps {
		for npp := range np {
			fmt.Fprintf(sb, "  %s-.->%s\n", n, npp)
		}
	}
	for _, node := range nodes {
		fmt.Fprintf(sb, "  %s\n", node.Descriptor())
	}
	return []byte(sb.String()), nil
}

var GlobalDebugHandler = new(DebugHandler)

type CollectionDump struct {
	// Map of output key -> output
	Outputs map[string]any `json:"outputs,omitempty"`
	// Name of the input collection
	InputCollection string `json:"inputCollection,omitempty"`
	// Map of input key -> info
	Inputs map[string]InputDump `json:"inputs,omitempty"`
}
type InputDump struct {
	Outputs      []string `json:"outputs,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
}
type DebugCollection struct {
	name string
	dump func() CollectionDump
}

func (p DebugCollection) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"name":  p.name,
		"state": p.dump(),
	})
}

// maybeRegisterCollectionForDebugging registers the collection in the debugger, if one is enabled
func maybeRegisterCollectionForDebugging[T any](c Collection[T], handler *DebugHandler) {
	if handler == nil {
		return
	}
	cc := c.(internalCollection[T])
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.debugCollections = append(handler.debugCollections, DebugCollection{
		name: cc.name(),
		dump: cc.dump,
	})
}

// nolint: unused // (not true, not sure why it thinks it is!)
func eraseMap[T any](l map[Key[T]]T) map[string]any {
	nm := make(map[string]any, len(l))
	for k, v := range l {
		nm[string(k)] = v
	}
	return nm
}
