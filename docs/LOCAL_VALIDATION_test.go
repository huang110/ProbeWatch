package docs

import (
	"os"
	"strings"
	"testing"
)

func TestLocalValidationDocumentsBindMountSetup(t *testing.T) {
	contents, err := os.ReadFile("LOCAL-VALIDATION.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(contents)
	for _, required := range []string{
		"cd deploy",
		"mkdir -p ../data",
		"chown 10001:10001 ../data",
		"chmod 700 ../data",
		"../data:/app/data",
		"ENTRYPOINT",
		"docker compose -f docker-compose.local.yml config",
		"docker compose -f docker-compose.local.yml build",
		"docker compose -f docker-compose.local.yml up",
		"New-Item -ItemType Directory -Force ..\\data",
		"does not automatically set Linux ownership",
	} {
		if !strings.Contains(doc, required) {
			t.Fatalf("LOCAL-VALIDATION.md is missing %q", required)
		}
	}
}

func TestDockerignoreProtectsSecretsAndPreservesBuildSources(t *testing.T) {
	contents, err := os.ReadFile("../.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	dockerignore := string(contents)
	for _, required := range []string{
		".env",
		".env.*",
		"!.env.example",
		"data/",
		"secrets/",
		"credentials/",
		"frontend/node_modules/",
		"frontend/dist/",
		".git/",
		"*.log",
		"*.key",
		"*.pem",
		"*.p12",
		"*.pfx",
	} {
		if !strings.Contains(dockerignore, required) {
			t.Fatalf(".dockerignore is missing %q", required)
		}
	}
	for _, source := range []string{"go.mod", "cmd/", "internal/"} {
		if strings.Contains(dockerignore, source) {
			t.Fatalf(".dockerignore would exclude required build source %q", source)
		}
	}
}
