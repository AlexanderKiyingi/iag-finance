// Package permissions builds the machine-readable RBAC manifest that the
// frontend (financeiag) consumes to validate and generate its permission
// constants, so the two repos cannot silently drift.
//
// The in-code declarations remain the single source of truth:
//   - the catalog comes from internal/models.PermissionDescriptors();
//   - the route gate map (added in phase 2) comes from the router gate table.
//
// Regenerate the committed manifest with: go run ./cmd/genmanifest
package permissions

import (
	"encoding/json"

	"github.com/iag-finance/backend/internal/models"
)

// ManifestVersion is bumped only when the manifest schema changes (not when its
// contents change); the frontend reads it to guard against schema mismatches.
const ManifestVersion = 1

// Permission is one catalog entry: a codename and its human description.
type Permission struct {
	Codename    string `json:"codename"`
	Description string `json:"description"`
}

// Manifest is the serialized projection of finance RBAC.
type Manifest struct {
	Version int          `json:"version"`
	Catalog []Permission `json:"catalog"`
	// RouteGates maps each mutating endpoint to the any-of permissions that
	// satisfy its gate, so the frontend can validate its form→permission map.
	RouteGates []RouteGate `json:"routeGates"`
}

// Build assembles the manifest from the in-code source of truth.
func Build() Manifest {
	descs := models.PermissionDescriptors()
	catalog := make([]Permission, 0, len(descs))
	for _, d := range descs {
		catalog = append(catalog, Permission{Codename: d.Name, Description: d.Description})
	}
	return Manifest{Version: ManifestVersion, Catalog: catalog, RouteGates: RouteGates()}
}

// JSON renders the manifest deterministically (stable order from the source
// slice, 2-space indent, trailing newline) so the committed file is diff-stable
// for the staleness guard in CI.
func (m Manifest) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
