# Copyright 2019 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

pwd := $(shell pwd)

TMPDIR := $(shell mktemp -d)

repo_dir := .
out_path = ${TMPDIR}
protoc = protoc -Icommon-protos -Ioperator

go_plugin_prefix := --go_out=plugins=grpc,
go_plugin := $(go_plugin_prefix):$(out_path)

comma := ,
empty :=
space := $(empty) $(empty)

importmaps := \
	gogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto \
	google/protobuf/any.proto=github.com/gogo/protobuf/types \
	google/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor \
	google/protobuf/duration.proto=github.com/gogo/protobuf/types \
	google/protobuf/struct.proto=github.com/gogo/protobuf/types \
	google/protobuf/timestamp.proto=github.com/gogo/protobuf/types \
	google/protobuf/wrappers.proto=github.com/gogo/protobuf/types \
	google/rpc/status.proto=istio.io/gogo-genproto/googleapis/google/rpc \
	google/rpc/code.proto=istio.io/gogo-genproto/googleapis/google/rpc \
	google/rpc/error_details.proto=istio.io/gogo-genproto/googleapis/google/rpc \
	google/api/field_behavior.proto=istio.io/gogo-genproto/googleapis/google/api \

# generate mapping directive with M<proto>:<go pkg>, format for each proto file
mapping_with_spaces := $(foreach map,$(importmaps),M$(map),)
gogo_mapping := $(subst $(space),$(empty),$(mapping_with_spaces))

gogofast_plugin_prefix := --gogofast_out=plugins=grpc,
gogofast_plugin := $(gogofast_plugin_prefix)$(gogo_mapping):$(out_path)
protoc_gen_docs_plugin := --docs_out=warnings=true,mode=html_fragment_with_front_matter:$(repo_dir)/

v1alpha1_path := operator/pkg/apis/istio/v1alpha1
v1alpha1_protos := $(wildcard $(v1alpha1_path)/*.proto)
v1alpha1_pb_gos := $(v1alpha1_protos:.proto=.pb.go)
v1alpha1_pb_docs := $(v1alpha1_path)/v1alpha1.pb.html
v1alpha1_openapi := $(v1alpha1_protos:.proto=.json)

$(v1alpha1_pb_gos) $(v1alpha1_pb_docs): $(v1alpha1_protos)
	@$(protoc) $(gogofast_plugin) $(protoc_gen_docs_plugin)$(v1alpha1_path) $^
	@cp -r ${TMPDIR}/pkg/* operator/pkg/
	@rm -fr ${TMPDIR}/pkg
	@go run $(repo_dir)/operator/pkg/apis/istio/fixup_structs/main.go -f $(v1alpha1_path)/values_types.pb.go

.PHONY: operator-proto
operator-proto: $(v1alpha1_pb_gos) $(v1alpha1_pb_docs)
