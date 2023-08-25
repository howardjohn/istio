package main

import (
	"istio.io/istio/pkg/log"
	"log/slog"
)

func main() {
	log.Configure(log.DefaultOptions())
	slog.Info("hello")
	slog.With("key", "value").Info("hello")
	slog.With(slog.Group("key", "baz", "value")).Info("hello")
}