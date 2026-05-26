package controllers

import (
	"net/http"

	"github.com/iag/finance-backend/internal/config"
	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/views"
)

type BootstrapController struct {
	store *models.Store
	cfg   config.Config
}

func NewBootstrapController(store *models.Store, cfg config.Config) *BootstrapController {
	return &BootstrapController{store: store, cfg: cfg}
}

func (c *BootstrapController) Bootstrap(w http.ResponseWriter, r *http.Request) {
	views.JSON(w, http.StatusOK, c.store.Bootstrap())
}

func (c *BootstrapController) Accounts(w http.ResponseWriter, r *http.Request) {
	views.JSON(w, http.StatusOK, c.store.ListAuthAccounts())
}

func (c *BootstrapController) Session(w http.ResponseWriter, r *http.Request) {
	views.JSON(w, http.StatusOK, models.LoginResponse{
		Session:     c.store.GetSession(),
		Permissions: c.store.PermissionContext(),
	})
}

func (c *BootstrapController) Login(w http.ResponseWriter, r *http.Request) {
	var in models.LoginInput
	if err := decodeJSON(r, &in); err != nil {
		views.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	sess, tokens, err := c.store.LoginWithTokens(in.Email, in.Password)
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusOK, models.LoginResponse{
		Session: sess, Permissions: c.store.PermissionContext(), Tokens: tokens,
	})
}

func (c *BootstrapController) Refresh(w http.ResponseWriter, r *http.Request) {
	var in models.RefreshInput
	if err := decodeJSON(r, &in); err != nil {
		views.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	sess, tokens, err := c.store.RefreshAccess(in.RefreshToken)
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusOK, models.LoginResponse{
		Session: sess, Permissions: c.store.PermissionContext(), Tokens: tokens,
	})
}

func (c *BootstrapController) Logout(w http.ResponseWriter, r *http.Request) {
	c.store.Logout()
	views.NoContent(w)
}

func (c *BootstrapController) PatchSession(w http.ResponseWriter, r *http.Request) {
	var patch models.SessionPatch
	if err := decodeJSON(r, &patch); err != nil {
		views.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	sess, err := c.store.PatchSession(patch)
	if err != nil {
		views.WriteError(w, err)
		return
	}
	views.JSON(w, http.StatusOK, map[string]any{"session": sess})
}

func (c *BootstrapController) ResetDemo(w http.ResponseWriter, r *http.Request) {
	if !c.cfg.AllowDemoReset {
		views.Error(w, http.StatusForbidden, "demo reset disabled")
		return
	}
	c.store.Reset()
	views.JSON(w, http.StatusOK, c.store.Bootstrap())
}
