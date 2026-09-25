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
			fmt.Println(version.FullAgentVersionString())
			return nil
		case "--check-update", "check-update":
			return runtime.CheckAgentUpdate()
		case "--self-update", "self-update", "upgrade":
			return runtime.SelfUpdateAgent()
		case "-h", "--help", "help":
			printUsage()
			return nil
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return runtime.StartAgent(cfg)
}

func printUsage() {
	fmt.Printf("%s\n\n", version.FullAgentVersionString())
	fmt.Println("Usage: probewatch-agent [command]")
	fmt.Println("\nCommands:")
	fmt.Println("  (no argument)    Start the monitoring agent background runner")
	fmt.Println("  -v, --version    Show current agent version and architecture")
	fmt.Println("  --check-update   Check control plane for available updates")
	fmt.Println("  --self-update    Download and apply latest update in place")
	fmt.Println("  -h, --help       Show this help message")
}
