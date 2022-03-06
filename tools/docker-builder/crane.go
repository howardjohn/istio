package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/asottile/dockerfile"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/moby/buildkit/frontend/dockerfile/shell"

	testenv "istio.io/istio/pkg/test/env"
	"istio.io/pkg/log"
)

type buildArgs struct {
	Env        map[string]string
	User       string
	Entrypoint []string
	Base       string
	Dest       string
	Data       string
}

var (
	bases   = map[string]v1.Image{}
	basesMu sync.RWMutex
)

type state struct {
	args       map[string]string
	bases      map[string]string
	copies     map[string]string
	user       string
	base       string
	shlex      *shell.Lex
	entrypoint []string
}

func (s state) ToCraneArgs(target, destHub string) (buildArgs, error) {
	base := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("build.docker.%s", target))
	dest := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("docker.%s-tar", target))
	destTar := filepath.Join(dest, "data.tar")
	for orig, dst := range s.copies {
		if err := CopyGeneric(filepath.Join(base, orig), filepath.Join(dest, dst)); err != nil {
			return buildArgs{}, err
		}
	}
	if err := VerboseCommand("tar", "cf", destTar, "-C", dest, "--exclude", "data.tar", ".").Run(); err != nil {
		return buildArgs{}, err
	}
	return buildArgs{
		Env:        nil,
		User:       s.user,
		Entrypoint: s.entrypoint,
		Base:       s.base,
		Dest:       destHub,
		Data:       destTar,
	}, nil
}

func (s state) Expand(i string) string {
	r, _ := s.shlex.ProcessWordWithMap(i, s.args)
	return r
}

func Cut(s, sep string) (before, after string) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):]
	}
	return s, ""
}

var parseLog = log.RegisterScope("parse", "", 0)

func parseDockerFile(f string, args map[string]string) (state, error) {
	cmds, err := dockerfile.ParseFile(f)
	if err != nil {
		return state{}, err
	}
	s := state{
		args:   map[string]string{},
		bases:  map[string]string{},
		copies: map[string]string{},
	}
	shlex := shell.NewLex('\\')
	s.shlex = shlex
	for k, v := range args {
		s.args[k] = v
	}
	for _, c := range cmds {
		switch c.Cmd {
		case "ARG":
			k, v := Cut(c.Value[0], "=")
			_, f := s.args[k]
			if !f {
				s.args[k] = v
			}
		case "FROM":
			img := c.Value[0]
			s.base = s.Expand(img)
			if a, f := s.bases[s.base]; f {
				s.base = a
			}
			if len(c.Value) == 3 { // FROM x as y
				s.bases[c.Value[2]] = s.base
			}
		case "COPY":
			src := s.Expand(c.Value[0])
			dst := s.Expand(c.Value[1])
			s.copies[src] = dst
		case "USER":
			s.user = c.Value[0]
		case "ENTRYPOINT":
			s.entrypoint = c.Value
		default:
			parseLog.Warnf("did not handle %+v", c)
		}
		parseLog.Debugf("%v: %+v", c.Original, s)
	}
	return s, nil
}

func loadBase(b string) error {
	ref, err := name.ParseReference(b)
	if err != nil {
		return err
	}
	bi, err := remote.Image(ref)
	if err != nil {
		return err
	}
	log.Infof("base loaded")
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
				log.Infof(u)
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
	if baseImage == nil {
		log.Warnf("on demand loading base image %q", args.Base)
		ref, err := name.ParseReference(args.Base)
		if err != nil {
			return err
		}
		bi, err := remote.Image(ref)
		if err != nil {
			return err
		}
		baseImage = bi
	}
	log.Infof("create base in %v", time.Since(t0))

	cfgFile, err := baseImage.ConfigFile()
	if err != nil {
		return err
	}

	log.Infof("base config in %v", time.Since(t0))

	cfg := cfgFile.Config
	for k, v := range args.Env {
		cfg.Env = append(cfg.Env, fmt.Sprintf("%v=%v", k, v))
	}
	if args.User != "" {
		cfg.User = args.User
	}
	if len(args.Entrypoint) > 0 {
		cfg.Entrypoint = args.Entrypoint
	}

	updated, err := mutate.Config(baseImage, cfg)
	if err != nil {
		return err
	}
	log.Infof("config in %v", time.Since(t0))

	l, err := tarball.LayerFromFile(args.Data, tarball.WithCompressedCaching)
	if err != nil {
		return err
	}
	log.Infof("read layer in %v", time.Since(t0))

	files, err := mutate.AppendLayers(updated, l)
	if err != nil {
		return err
	}

	log.Infof("layer in %v", time.Since(t0))

	destRef, err := name.ParseReference(args.Dest)
	if err != nil {
		return err
	}

	if err := remote.Write(destRef, files); err != nil {
		return err
	}

	log.Infof("write in %v", time.Since(t0))
	return nil
}
