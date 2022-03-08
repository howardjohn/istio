package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
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
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/moby/buildkit/frontend/dockerfile/shell"

	testenv "istio.io/istio/pkg/test/env"
	"istio.io/pkg/log"
)

type buildArgs struct {
	Env        map[string]string
	User       string
	WorkDir    string
	Entrypoint []string
	Base       string
	Dest       string
	Data       string
	DataDir    string
}

var (
	bases   = map[string]v1.Image{}
	basesMu sync.RWMutex
)

type state struct {
	args       map[string]string
	env        map[string]string
	bases      map[string]string
	copies     map[string]string
	user       string
	workdir    string
	base       string
	shlex      *shell.Lex
	entrypoint []string
}

func Symlink(oldname, newname string) error {
	if err := os.MkdirAll(filepath.Dir(newname), 0o755); err != nil {
		return err
	}
	_ = os.Remove(newname)
	return os.Symlink(oldname, newname)
}

func (s state) ToCraneArgs(target, destHub string) (buildArgs, error) {
	t0 := time.Now()
	defer func() {
		log.WithLabels("image", target, "total", time.Since(t0), "step", time.Since(t0)).Info("copy")
	}()
	base := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("build.docker.%s", target))
	dest := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("docker.%s-tar", target))
	for orig, dst := range s.copies {
		if err := Symlink(filepath.Join(base, orig), filepath.Join(dest, dst)); err != nil {
			return buildArgs{}, err
		}
	}
	return buildArgs{
		Env:        s.env,
		User:       s.user,
		WorkDir:    s.workdir,
		Entrypoint: s.entrypoint,
		Base:       s.base,
		Dest:       destHub,
		DataDir:    dest,
	}, nil
}

func (s state) Expand(i string) string {
	avail := map[string]string{}
	for k, v := range s.args {
		avail[k] = v
	}
	for k, v := range s.env {
		avail[k] = v
	}
	r, _ := s.shlex.ProcessWordWithMap(i, avail)
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
	t0 := time.Now()
	defer func() {
		log.WithLabels("image", f, "total", time.Since(t0), "step", time.Since(t0)).Info("parse")
	}()
	cmds, err := dockerfile.ParseFile(f)
	if err != nil {
		return state{}, err
	}
	s := state{
		args:   map[string]string{},
		env:    map[string]string{},
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
		case "ENV":
			k := s.Expand(c.Value[0])
			v := s.Expand(c.Value[1])
			s.env[k] = v
		case "WORKDIR":
			v := s.Expand(c.Value[0])
			s.workdir = v
		default:
			parseLog.Warnf("did not handle %+v", c)
		}
		parseLog.Debugf("%v: %+v", filepath.Base(c.Original), s)
	}
	return s, nil
}

func loadBase(b string) error {
	t0 := time.Now()
	ref, err := name.ParseReference(b)
	if err != nil {
		return err
	}
	bi, err := remote.Image(ref)
	if err != nil {
		return err
	}
	log.WithLabels("step", time.Since(t0)).Infof("base loaded")
	bases[b] = bi
	return nil
}

func ByteCount(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

func build(args buildArgs) error {
	t0 := time.Now()
	lt := t0
	trace := func(d string) {
		log.WithLabels("image", args.Dest, "total", time.Since(t0), "step", time.Since(lt)).Info(d)
		lt = time.Now()
	}
	if args.Dest == "" {
		return fmt.Errorf("dest required")
	}
	if args.Data == "" && args.DataDir == "" {
		return fmt.Errorf("data or datadir required")
	}

	updates := make(chan v1.Update, 1000)
	go func() {
		for {
			lastLog := time.Now()
			select {
			case u := <-updates:
				if time.Since(lastLog) < time.Second {
					// Limit to 1 log per image per second
					continue
				}
				lastLog = time.Now()
				log.WithLabels("image", args.Dest).Infof("%s/%s", ByteCount(u.Complete), ByteCount(u.Total))
			}
		}
	}()

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
		// bi, err := remote.Image(ref, remote.WithProgress(updates))
		if err != nil {
			return err
		}
		baseImage = bi
	}
	trace("create base")

	cfgFile, err := baseImage.ConfigFile()
	if err != nil {
		return err
	}

	trace("base config")

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
	if args.WorkDir != "" {
		cfg.WorkingDir = args.WorkDir
	}

	updated, err := mutate.Config(baseImage, cfg)
	if err != nil {
		return err
	}
	trace("config")

	var l v1.Layer
	if args.Data != "" {
		l, err = tarball.LayerFromFile(args.Data, tarball.WithCompressionLevel(gzip.NoCompression))
		if err != nil {
			return err
		}
		trace("read layer")
	} else {

		pr, pw := io.Pipe()
		go func() {
			err := WriteArchive(args.DataDir, pw, time.Now())
			_ = pw.Close()
			if err != nil {
				log.Errorf("failed to construct tar:v", err)
			}
			trace("stream layer")
		}()

		l = stream.NewLayer(pr, stream.WithCompressionLevel(gzip.NoCompression))
	}

	files, err := mutate.AppendLayers(updated, l)
	if err != nil {
		return err
	}

	trace("layer")

	destRef, err := name.ParseReference(args.Dest)
	if err != nil {
		return err
	}

	if err := remote.Write(destRef, files); err != nil {
		// if err := remote.Write(destRef, files, remote.WithProgress(updates)); err != nil {
		return err
	}

	trace("write")
	return nil
}
