#!/usr/bin/env bash

# Copyright 2020 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

readonly SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE}")"/.. && pwd)"

readonly GOMODCACHE="$(go env GOMODCACHE)"
readonly GOPATH="$(mktemp -d)"
export GOPATH GOMODCACHE

# Even when modules are enabled, the code-generator tools always write to
# a traditional GOPATH directory, so fake on up to point to the current
# workspace.
mkdir -p "$GOPATH/src/istio.io"
ln -s "${SCRIPT_ROOT}" "$GOPATH/src/istio.io/istio"

readonly OUTPUT_PKG=istio.io/istio/servicev2/pkg/client
readonly APIS_PKG=istio.io/istio/servicev2
readonly CLIENTSET_NAME=versioned
readonly CLIENTSET_PKG_NAME=clientset

echo "Generating CRDs"
go run ./servicev2/pkg/generator

readonly COMMON_FLAGS=" --go-header-file ${SCRIPT_ROOT}/servicev2/boilerplate.txt"

echo "Generating clientset at ${OUTPUT_PKG}/${CLIENTSET_PKG_NAME}"
client-gen \
  --clientset-name "${CLIENTSET_NAME}" \
  --input-base "" \
  --input "${APIS_PKG}/apis/v1" \
  --output-package "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME}" \
  ${COMMON_FLAGS}

echo "Generating listers at ${OUTPUT_PKG}/listers"
lister-gen \
  --input-dirs "${APIS_PKG}/apis/v1" \
  --output-package "${OUTPUT_PKG}/listers" \
  ${COMMON_FLAGS}

echo "Generating informers at ${OUTPUT_PKG}/informers"
informer-gen \
  --input-dirs "${APIS_PKG}/apis/v1" \
  --versioned-clientset-package "${OUTPUT_PKG}/${CLIENTSET_PKG_NAME}/${CLIENTSET_NAME}" \
  --listers-package "${OUTPUT_PKG}/listers" \
  --output-package "${OUTPUT_PKG}/informers" \
  ${COMMON_FLAGS}

for VERSION in v1
do
  echo "Generating ${VERSION} register at ${APIS_PKG}/apis/${VERSION}"
  register-gen \
    --input-dirs "${APIS_PKG}/apis/${VERSION}" \
    --output-package "${APIS_PKG}/apis/${VERSION}" \
    ${COMMON_FLAGS}

  echo "Generating ${VERSION} deepcopy at ${APIS_PKG}/apis/${VERSION}"
  controller-gen \
    object:headerFile=${SCRIPT_ROOT}/servicev2/boilerplate.txt \
    paths="${APIS_PKG}/apis/${VERSION}"

done
echo $APIS_PKG $GOPATH
