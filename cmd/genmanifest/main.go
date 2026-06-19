// Command genmanifest writes the finance RBAC manifest (docs/finance-permissions.manifest.json)
// consumed by the frontend. Run from the module root:
//
//	go run ./cmd/genmanifest
//
// CI runs the staleness test in internal/permissions to ensure the committed
// file stays in sync with the in-code source of truth.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/iag-finance/backend/internal/permissions"
)

const outPath = "docs/finance-permissions.manifest.json"

func main() {
	m := permissions.Build()
	data, err := m.JSON()
	if err != nil {
		log.Fatalf("build manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", filepath.Dir(outPath), err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", outPath, err)
	}
	log.Printf("wrote %s (%d permissions)", outPath, len(m.Catalog))
}
