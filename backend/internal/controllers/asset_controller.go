package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/query"
	"github.com/iag/finance-backend/internal/views"
)

type AssetController struct {
	store *models.Store
}

func NewAssetController(store *models.Store) *AssetController {
	return &AssetController{store: store}
}

func (c *AssetController) List(w http.ResponseWriter, r *http.Request) {
	p := query.ParsePage(r, 20, 100)
	resp, _ := c.store.ListAssets(p)
	views.JSON(w, http.StatusOK, resp)
}

func (c *AssetController) Get(w http.ResponseWriter, r *http.Request) {
	a, err := c.store.GetAsset(lastPathSegment(r))
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusOK, a)
}
