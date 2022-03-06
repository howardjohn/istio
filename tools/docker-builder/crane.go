package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

type buildArgs struct {
	Env        map[string]string
	User       string
	Entrypoint string
	Base       string
	Dest       string
	Data       string
}

var (
	bases   = map[string]v1.Image{}
	basesMu sync.RWMutex
)

func loadBase(b string) error {
	ref, err := name.ParseReference(b)
	if err != nil {
		return err
	}
	bi, err := remote.Image(ref)
	if err != nil {
		return err
	}
	log.Println("base loaded")
	bases[b] = bi
	return nil
}

func build(args buildArgs) error {
	if args.Dest == "" {
		return fmt.Errorf("dest required")
	}
	if args.Data == "" {
		return fmt.Errorf("data required")
	}

	updates := make(chan v1.Update)
	go func() {
		for {
			select {
			case u := <-updates:
				log.Println(u)
			}
		}
	}()

	t0 := time.Now()
	baseImage := empty.Image
	if args.Base != "" {
		basesMu.RLock()
		baseImage = bases[args.Base]
		basesMu.RUnlock()
	}
	log.Println("create base in ", time.Since(t0))

	cfgFile, err := baseImage.ConfigFile()
	if err != nil {
		return err
	}

	log.Println("base config in ", time.Since(t0))

	cfg := cfgFile.Config
	for k, v := range args.Env {
		cfg.Env = append(cfg.Env, fmt.Sprintf("%v=%v", k, v))
	}
	if args.User != "" {
		cfg.User = args.User
	}
	if args.Entrypoint != "" {
		cfg.Entrypoint = []string{args.Entrypoint}
	}

	updated, err := mutate.Config(baseImage, cfg)
	if err != nil {
		return err
	}
	log.Println("config in ", time.Since(t0))

	l, err := tarball.LayerFromFile(args.Data, tarball.WithCompressedCaching)
	if err != nil {
		return err
	}
	log.Println("read layer in ", time.Since(t0))

	files, err := mutate.AppendLayers(updated, l)
	if err != nil {
		return err
	}

	log.Println("layer in ", time.Since(t0))

	destRef, err := name.ParseReference(args.Dest)
	if err != nil {
		return err
	}

	if err := remote.Write(destRef, files); err != nil {
		return err
	}

	log.Println("write in ", time.Since(t0))
	return nil
}
