package kube

import (
	"os"

	"github.com/dominikbraun/graph"
	"istio.io/pkg/log"
)

type DAG struct {
	graph graph.Graph[string, string]
}

func NewDAG() *DAG {
	g := graph.New(graph.StringHash, graph.Directed())
	return &DAG {
		graph: g,
	}
}

type Registerer interface {
	AddDependency(chain []string)
}

func (d *DAG) X() {
	Mermaid(d.graph, os.Stdout)
}
// 0 -> 1 -> 2 ..
func (d *DAG) AddDependency(chain []string) {
	log.Errorf("howardjohn: register %v", chain)
	for i := range chain[:len(chain)-1] {
		d.graph.AddVertex(chain[i])
		d.graph.AddVertex(chain[i+1])
		d.graph.AddEdge(chain[i+1], chain[i])
	}
}

