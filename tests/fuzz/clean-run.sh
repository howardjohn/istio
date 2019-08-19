#!/usr/bin/env bash

set -ex

FUNC="${1:?func to run}"

cd $(dirname $0); pwd

rm -rf out
mkdir -p out/corpus

for file in `ls testdata`; do
  go run tools/yaml-to-bytes.go "testdata/$file" > "out/corpus/$file"
done

go-fuzz-build

go-fuzz -bin fuzz-fuzz.zip -workdir out -func "${FUNC}" -v 5
