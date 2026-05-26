package persistence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/iag/finance-backend/internal/models"
)

type FileSnapshot struct {
	path string
}

func NewFileSnapshot(path string) *FileSnapshot {
	return &FileSnapshot{path: path}
}

func (f *FileSnapshot) LoadState(ctx context.Context) (*models.PersistedState, error) {
	_ = ctx
	b, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, models.ErrNotFound
		}
		return nil, err
	}
	var st models.PersistedState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (f *FileSnapshot) SaveState(ctx context.Context, state *models.PersistedState) error {
	_ = ctx
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f *FileSnapshot) IsEmpty(ctx context.Context) (bool, error) {
	_ = ctx
	_, err := os.Stat(f.path)
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}

func (f *FileSnapshot) FindAuthAccount(ctx context.Context, email string) (models.AuthAccount, error) {
	return models.AuthAccount{}, models.ErrNotFound
}

func (f *FileSnapshot) ListAuthAccounts(ctx context.Context) ([]models.AuthAccount, error) {
	return nil, models.ErrNotFound
}
