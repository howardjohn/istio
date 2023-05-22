package main

import (
	slog "golang.org/x/exp/slog"
	"istio.io/istio/pkg/log"
)

func main() {
	opts := log.DefaultOptions()
	log.Configure(opts)
	slog.Info("plain")
	slog.Info("hello", "world", 1, "foo", "bar")
	slog.With("world", 1, "foo", "bar").Info("hello")
	log.Info("plain")
	log.WithLabels("world", 1, "foo", "bar").Info("hello")
}
