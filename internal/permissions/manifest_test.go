package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

// TestManifestNotStale fails if the committed manifest is out of sync with the
// in-code source of truth. Regenerate with: go run ./cmd/genmanifest
func TestManifestNotStale(t *testing.T) {
	want, err := Build().JSON()
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	// Test cwd is the package dir (internal/permissions); the manifest lives at
	// the module root under docs/.
	path := filepath.Join("..", "..", "docs", "finance-permissions.manifest.json")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate: go run ./cmd/genmanifest)", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is stale — regenerate with: go run ./cmd/genmanifest", path)
	}
}

// TestCatalogNonEmpty is a cheap sanity check that the source descriptors are
// present (guards against an accidental empty catalog reaching the frontend).
func TestCatalogNonEmpty(t *testing.T) {
	if len(Build().Catalog) == 0 {
		t.Fatal("permission catalog is empty")
	}
}
