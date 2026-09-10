package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/alvor-technologies/iag-platform-go/apierr"
	"github.com/iag-finance/backend/internal/appkv"
)

// maxAppKVBytes bounds one stored document.
//
// These namespaces hold settings, a form draft or a push subscription — all
// small. Without a bound, `kv` in particular is an open invitation to park
// megabytes of client state in the finance database.
const maxAppKVBytes = 256 * 1024

// appKVUser returns the caller's platform user id for user-scoped namespaces.
// Global namespaces do not need one.
func (a *API) appKVUser(c *gin.Context, namespace string) (string, bool) {
	if appkv.IsGlobal(namespace) {
		return "", true
	}
	id, ok := portalUserID(c)
	if !ok {
		apierr.JSONStatus(c, http.StatusUnauthorized, "a signed-in user is required for this namespace")
		return "", false
	}
	return id.String(), true
}

func (a *API) appKVReady(c *gin.Context) bool {
	if a.AppKV == nil {
		apierr.JSONStatus(c, http.StatusServiceUnavailable, "app store unavailable")
		return false
	}
	return true
}

// resolveNamespace validates the path segment against the served whitelist and
// checks it against the branch of the router it arrived on.
//
// The two branches carry different authorization: /app/global/* is gated on an
// administrative permission, /app/me/* is authorized by ownership alone. If a
// namespace could be reached through either, writing tenant-wide settings would
// only require being signed in — so the declared scope and the route prefix
// must agree, and disagreeing is a 404 rather than a redirect.
func resolveNamespace(c *gin.Context) (string, bool) {
	ns := strings.TrimSpace(c.Param("namespace"))
	scope, ok := appkv.ScopeFor(ns)
	if !ok {
		apierr.JSONStatus(c, http.StatusNotFound, "unknown namespace "+ns)
		return "", false
	}
	viaGlobal := strings.Contains(c.FullPath(), "/app/global/")
	if viaGlobal != (scope == appkv.ScopeGlobal) {
		apierr.JSONStatus(c, http.StatusNotFound,
			"namespace "+ns+" is not served on this path")
		return "", false
	}
	return ns, true
}

// ListAppKV answers every document in a namespace, scoped to the caller.
func (a *API) ListAppKV(c *gin.Context) {
	if !a.appKVReady(c) {
		return
	}
	ns, ok := resolveNamespace(c)
	if !ok {
		return
	}
	user, ok := a.appKVUser(c, ns)
	if !ok {
		return
	}
	docs, err := a.AppKV.List(c.Request.Context(), ns, user)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not list "+ns)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": docs})
}

// GetAppKV answers one document.
func (a *API) GetAppKV(c *gin.Context) {
	if !a.appKVReady(c) {
		return
	}
	ns, ok := resolveNamespace(c)
	if !ok {
		return
	}
	user, ok := a.appKVUser(c, ns)
	if !ok {
		return
	}
	doc, err := a.AppKV.Get(c.Request.Context(), ns, c.Param("key"), user)
	if errors.Is(err, appkv.ErrNotFound) {
		apierr.JSONStatus(c, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not read "+ns)
		return
	}
	c.JSON(http.StatusOK, doc)
}

// PutAppKV upserts one document.
//
// The body is taken as raw JSON rather than bound to a struct: these documents
// are the app's own shapes (a settings blob, a draft form, a push
// subscription) and this service has no business asserting what is inside them.
// It validates that the body *is* JSON and that it is not enormous, and stores
// it.
func (a *API) PutAppKV(c *gin.Context) {
	if !a.appKVReady(c) {
		return
	}
	ns, ok := resolveNamespace(c)
	if !ok {
		return
	}
	user, ok := a.appKVUser(c, ns)
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxAppKVBytes+1))
	if err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, "could not read body")
		return
	}
	if len(body) > maxAppKVBytes {
		apierr.JSONStatus(c, http.StatusRequestEntityTooLarge, "document exceeds 256 KB")
		return
	}
	if !json.Valid(body) {
		apierr.JSONStatus(c, http.StatusBadRequest, "body must be JSON")
		return
	}

	doc, err := a.AppKV.Put(c.Request.Context(), ns, c.Param("key"), body, user, chainActor(c))
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not save "+ns)
		return
	}
	c.JSON(http.StatusOK, doc)
}

// DeleteAppKV removes one document.
func (a *API) DeleteAppKV(c *gin.Context) {
	if !a.appKVReady(c) {
		return
	}
	ns, ok := resolveNamespace(c)
	if !ok {
		return
	}
	user, ok := a.appKVUser(c, ns)
	if !ok {
		return
	}
	if err := a.AppKV.Delete(c.Request.Context(), ns, c.Param("key"), user); err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, "could not delete from "+ns)
		return
	}
	c.Status(http.StatusNoContent)
}
