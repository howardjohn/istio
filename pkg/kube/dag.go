package kube

import (
	"os"
	"strings"

	"github.com/dominikbraun/graph"

	"istio.io/pkg/log"
)

type DAG struct {
	graph graph.Graph[string, string]
}

func NewDAG() *DAG {
	g := graph.New(graph.StringHash, graph.Directed())
	return &DAG{
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
func (d *DAG) AddDependency(raw []string) {
	log.Errorf("howardjohn: register %v", raw)
	chain := Filter(raw, func(s string) bool {
		return !strings.HasPrefix(s, "_")
	})
	for i := range chain[:len(chain)-1] {
		d.graph.AddVertex(chain[i])
		d.graph.AddVertex(chain[i+1])
		d.graph.AddEdge(chain[i+1], chain[i])
	}
}

func Filter[T any](data []T, f func(T) bool) []T {
	fltd := make([]T, 0, len(data))
	for _, e := range data {
		if f(e) {
			fltd = append(fltd, e)
		}
	}
	return fltd
}
