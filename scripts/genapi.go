//go:build ignore

// Command genapi regenerates the internal/api package with ogen from the
// bdrive-docs OpenAPI spec.
//
// The spec is fetched from a bdrive-docs branch that, by default, matches the
// branch currently being built. This keeps the generated API in sync with the
// docs changes that belong to the same feature branch (for example, building
// the "deduplication" branch pulls the spec from bdrive-docs/deduplication),
// instead of always pulling from main and breaking the build whenever backend
// code lands before the matching docs change is merged.
//
// The ref can be overridden explicitly, and falls back to main when no matching
// docs branch exists. Resolution order:
//
//  1. BDRIVE_DOCS_REF        explicit override
//  2. GITHUB_HEAD_REF        PR source branch (GitHub Actions)
//  3. GITHUB_REF_NAME        pushed branch/tag (GitHub Actions)
//  4. current local git branch
//  5. "main"
//
// If the resolved ref has no spec at bdrive-docs (HTTP 404), genapi falls back
// to main so branches without a docs counterpart still build.
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

const (
	docsRawBase = "https://raw.githubusercontent.com/BenjiThatFoxGuy/bdrive-docs"
	specPath    = "openapi/openapi.json"
	defaultRef  = "main"
)

func specURL(ref string) string {
	return fmt.Sprintf("%s/%s/%s", docsRawBase, ref, specPath)
}

func main() {
	ref := resolveRef()
	url := specURL(ref)

	if ref != defaultRef && !specExists(url) {
		fmt.Fprintf(os.Stderr, "genapi: no spec at bdrive-docs ref %q, falling back to %q\n", ref, defaultRef)
		ref = defaultRef
		url = specURL(ref)
	}

	fmt.Fprintf(os.Stderr, "genapi: generating internal/api from %s\n", url)

	cmd := exec.Command("go", "run", "github.com/ogen-go/ogen/cmd/ogen",
		"--clean", "--package", "api", "--target", "internal/api", url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "genapi: ogen failed: %v\n", err)
		os.Exit(1)
	}
}

// resolveRef picks the bdrive-docs branch to pull the OpenAPI spec from.
func resolveRef() string {
	for _, env := range []string{"BDRIVE_DOCS_REF", "GITHUB_HEAD_REF", "GITHUB_REF_NAME"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	if out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" && b != "HEAD" {
			return b
		}
	}
	return defaultRef
}

// specExists reports whether the spec is reachable at url (HTTP 200).
func specExists(url string) bool {
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
