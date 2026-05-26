package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/views"
)

func requirePerm(store *models.Store, w http.ResponseWriter, key string) bool {
	if err := store.RequirePermission(key); err != nil {
		views.WriteError(w, err)
		return false
	}
	return true
}
