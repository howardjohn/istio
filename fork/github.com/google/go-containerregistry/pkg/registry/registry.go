// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package registry implements a docker V2 registry and the OCI distribution specification.
//
// It is designed to be used anywhere a low dependency container registry is needed, with an
// initial focus on tests.
//
// Its goal is to be standards compliant and its strictness will increase over time.
//
// This is currently a low flightmiles system. It's likely quite safe to use in tests; If you're using it
// in production, please let us know how and send us CL's for integration tests.
package registry

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

type readGetter func() (io.ReadCloser, error)

type registry struct {
	log       *log.Logger
	blobs     blobs
	manifests manifests
	images    []NamedIndex
}

// https://docs.docker.com/registry/spec/api/#api-version-check
// https://github.com/opencontainers/distribution-spec/blob/master/spec.md#api-version-check
func (r *registry) v2(resp http.ResponseWriter, req *http.Request) *regError {
	if isBlob(req) {
		r.log.Println("is blob")
		return r.blobs.handle(resp, req)
	}
	if isManifest(req) {
		r.log.Println("is manifest")
		return r.manifests.handle(resp, req)
	}
	if isTags(req) {
		return r.manifests.handleTags(resp, req)
	}
	if isCatalog(req) {
		return r.manifests.handleCatalog(resp, req)
	}
	resp.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	if req.URL.Path != "/v2/" && req.URL.Path != "/v2" {
		return &regError{
			Status:  http.StatusNotFound,
			Code:    "METHOD_UNKNOWN",
			Message: "We don't understand your method + url",
		}
	}
	resp.WriteHeader(200)
	return nil
}

func (r *registry) root(resp http.ResponseWriter, req *http.Request) {
	if rerr := r.v2(resp, req); rerr != nil {
		r.log.Printf("%s %s %d %s %s", req.Method, req.URL, rerr.Status, rerr.Code, rerr.Message)
		rerr.Write(resp)
		return
	}
	r.log.Printf("%s %s", req.Method, req.URL)
}

// New returns a handler which implements the docker registry protocol.
// It should be registered at the site root.
func New(opts ...Option) http.Handler {
	r := &registry{
		log: log.New(os.Stderr, "", log.LstdFlags),
		blobs: blobs{
			blobHandler: &memHandler{m: map[string][]byte{}},
			uploads:     map[string][]byte{},
			known:       map[string]readGetter{},
		},
		manifests: manifests{
			manifests: map[string]map[string]manifest{},
			log:       log.New(os.Stderr, "", log.LstdFlags),
		},
	}
	for _, o := range opts {
		o(r)
	}
	return http.HandlerFunc(r.root)
}

// Option describes the available options
// for creating the registry.
type Option func(r *registry)

// Logger overrides the logger used to record requests to the registry.
func Logger(l *log.Logger) Option {
	return func(r *registry) {
		r.log = l
		r.manifests.log = l
	}
}

type NamedIndex struct {
	Name  name.Tag
	Index v1.ImageIndex
	Image v1.Image
}

// WithImages preloads some images
func WithImages(imgs ...NamedIndex) Option {
	return func(r *registry) {
		r.images = imgs
		r.manifests.images = imgs
		for _, i := range imgs {
			d, _ := i.Image.Digest()
			m, _ := i.Image.MediaType()
			rm, _ := i.Image.RawManifest()

			r.manifests.manifests[i.Name.RepositoryStr()] = map[string]manifest{
				d.String(): {
					contentType: string(m),
					blob:        rm,
				},
				i.Name.TagStr(): {
					contentType: string(m),
					blob:        rm,
				},
			}

			layers, _ := i.Image.Layers()
			for _, layer := range layers {
				d, _ := layer.Digest()
				r.blobs.known[d.String()] = layer.Compressed
				//cr, _ := layer.Compressed()
				//r.log.Println("insert blob", d, r.blobs.blobHandler.(*memHandler).Put(nil, "", d, cr))
			}
			// Write the config.
			cfgName, _ := i.Image.ConfigName()
			cfgBlob, _ := i.Image.RawConfigFile()
			r.blobs.known[cfgName.String()] = func() (io.ReadCloser, error) {
				return ioutil.NopCloser(bytes.NewReader(cfgBlob)), nil
			}
			//r.log.Println("insert config blob", d, r.blobs.blobHandler.(*memHandler).Put(nil, "", cfgName, ioutil.NopCloser(bytes.NewReader(cfgBlob))))

			// Write the img manifest.
			dd, _ := i.Image.Digest()
			md, _ := i.Image.RawManifest()
			r.blobs.known[dd.String()] = func() (io.ReadCloser, error) {
				return ioutil.NopCloser(bytes.NewReader(md)), nil
			}
			//r.log.Println("insert manifest blob", d, r.blobs.blobHandler.(*memHandler).Put(nil, "", dd, ioutil.NopCloser(bytes.NewReader(md))))
		}
	}
}
