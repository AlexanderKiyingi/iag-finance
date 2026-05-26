package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/views"
)

type SettingsController struct {
	store *models.Store
}

func NewSettingsController(store *models.Store) *SettingsController {
	return &SettingsController{store: store}
}

func (c *SettingsController) Get(w http.ResponseWriter, r *http.Request) {
	views.JSON(w, http.StatusOK, c.store.GetSettings())
}

func (c *SettingsController) Patch(w http.ResponseWriter, r *http.Request) {
	if err := c.store.RequirePermission("settings.write"); err != nil {
		views.WriteError(w, err)
		return
	}
	var patch map[string]string
	if err := decodeJSON(r, &patch); err != nil {
		views.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	views.JSON(w, http.StatusOK, c.store.PatchSettings(patch))
}
