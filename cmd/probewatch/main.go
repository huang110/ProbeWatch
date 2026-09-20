package main

import (
	"log"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/runtime"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return runtime.StartControlPlane(cfg)
}
