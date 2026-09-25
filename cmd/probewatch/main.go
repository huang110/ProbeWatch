package main

import (
	"fmt"
	"log"
	"os"

	"github.com/probewatch/probewatch/internal/config"
	"github.com/probewatch/probewatch/internal/runtime"
	"github.com/probewatch/probewatch/internal/version"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Printf("ProbeWatch Server v%s\n", version.ServerVersion)
			return nil
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return runtime.StartControlPlane(cfg)
}
