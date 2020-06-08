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

// Tool to generate pilot/pkg/config/kube/types.go
// Example run command:
// go run pilot/pkg/config/kube/crd/codegen/types.go --template pilot/pkg/config/kube/crd/codegen/types.go.tmpl --output pilot/pkg/config/kube/crd/types.gen.go
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"text/template"

	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// ConfigData is data struct to feed to types.go template.
type ConfigData struct {
	VariableName string
	Import       string
	Kind         string
}

// MakeConfigData prepare data for code generation for the given schema.
func MakeConfigData(schema collection.Schema) ConfigData {
	pkg := schema.Resource().ProtoPackage()
	spl := strings.Split(pkg, "/")
	importName := strings.Join(spl[len(spl)-2:len(spl)], "_")

	out := ConfigData{
		VariableName: schema.VariableName(),
		Import:       importName,
		Kind:         schema.Resource().Kind(),
	}
	log.Printf("Generating Istio type %s for %s/%s CRD\n", out.VariableName, out.Import, out.Kind)
	return out
}

func main() {
	templateFile := flag.String("template", "pilot/tools/types.go.tmpl", "Template file")
	outputFile := flag.String("output", "", "Output file. Leave blank to go to stdout")
	flag.Parse()

	tmpl := template.Must(template.ParseFiles(*templateFile))

	// Prepare to generate types for mock schema and all Istio schemas
	typeList := []ConfigData{}
	for _, s := range collections.Pilot.All() {
		typeList = append(typeList, MakeConfigData(s))
	}
	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, typeList); err != nil {
		log.Fatal(fmt.Errorf("template: %v", err))
	}

	// Output
	if outputFile == nil || *outputFile == "" {
		fmt.Println(buffer.String())
	} else if err := ioutil.WriteFile(*outputFile, buffer.Bytes(), 0644); err != nil {
		panic(err)
	}
}
