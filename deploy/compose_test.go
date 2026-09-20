package deploy

// This dependency-free test is a structural contract for the local Compose file.
// When Docker is installed, also run `docker compose -f docker-compose.local.yml config`.

import (
	"os"
	"strings"
	"testing"
)

func TestLocalComposeKeepsHostLoopbackAndContainerListenSeparate(t *testing.T) {
	contents, err := os.ReadFile("docker-compose.local.yml")
	if err != nil {
		t.Fatal(err)
	}
	compose := string(contents)

	if !strings.Contains(compose, "services:\n  probewatch:\n") {
		t.Fatal("compose is missing the probewatch service")
	}
	if !strings.Contains(compose, "    ports:\n      - \"127.0.0.1:8080:8080\"\n") {
		t.Fatal("compose must publish the host port on loopback")
	}
	if !strings.Contains(compose, "    environment:\n") {
		t.Fatal("compose is missing the service environment block")
	}
	if !strings.Contains(compose, "      PROBEWATCH_LISTEN: 0.0.0.0:8080\n") {
		t.Fatal("compose must configure the container-internal listen address")
	}
	if !strings.Contains(compose, "      PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN: true\n") {
		t.Fatal("compose must explicitly opt into the container-internal non-loopback listen")
	}
	if !strings.Contains(compose, "      PROBEWATCH_DEPLOYMENT_MODE: container\n") {
		t.Fatal("compose must explicitly declare container deployment mode")
	}
	if strings.Contains(compose, "    command:") {
		t.Fatal("compose must not duplicate the Dockerfile entrypoint")
	}
	if strings.Count(compose, "PROBEWATCH_LISTEN:") != 1 || strings.Count(compose, "PROBEWATCH_ALLOW_NON_LOOPBACK_LISTEN:") != 1 || strings.Count(compose, "PROBEWATCH_DEPLOYMENT_MODE:") != 1 {
		t.Fatal("compose must define each listen-safety environment variable exactly once")
	}
	if !strings.Contains(compose, "    build:\n      context: ..\n      dockerfile: deploy/Dockerfile.local\n") {
		t.Fatal("compose must build the local image from the repository root")
	}
	if strings.Contains(compose, "\n      - ./data:/app/data") {
		t.Fatal("compose uses a deploy-relative data path")
	}
	if strings.Contains(compose, "image: probewatch:local") {
		t.Fatal("compose requires a pre-existing image")
	}
	if strings.Count(compose, "0.0.0.0") != 1 {
		t.Fatal("0.0.0.0 must appear exactly once for container-internal listen")
	}
	if strings.Count(compose, "- ../data:/app/data") != 1 {
		t.Fatal("compose must contain exactly one data volume")
	}
	if strings.Count(compose, "    volumes:\n") != 1 || strings.Count(compose, "    environment:\n") != 1 || strings.Count(compose, "    ports:\n") != 1 {
		t.Fatal("compose must have one service environment, ports, and volumes section")
	}
	for _, forbidden := range []string{
		"/var/run/docker.sock",
		"docker.sock",
		"privileged:",
		"network_mode: host",
		"- /:/",
		"- /root:",
	} {
		if strings.Contains(strings.ToLower(compose), strings.ToLower(forbidden)) {
			t.Fatalf("compose contains forbidden setting %q", forbidden)
		}
	}

	dockerfile, err := os.ReadFile("Dockerfile.local")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"FROM golang:1.23-alpine AS build",
		"go build -o /out/probewatch ./cmd/probewatch",
		"COPY --from=build /out/probewatch /probewatch",
		"RUN adduser",
	} {
		if !strings.Contains(string(dockerfile), required) {
			t.Fatalf("Dockerfile.local is missing %q", required)
		}
	}
}
