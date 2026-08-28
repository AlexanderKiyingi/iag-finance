package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Attachments for finance records (bank statements, supporting documents for a
// journal or an invoice) live in the shared public.attachments table, keyed by
// owner_ref - the record's own reference.
//
// Bytes never pass through this service: create returns a presigned PUT and
// download returns a presigned GET, so the browser talks to the bucket directly
// and a large upload does not occupy a service goroutine for its duration.
//
// The metadata row is written BEFORE the object exists. Writing it after a
// successful upload would depend on a second call the client may never make,
// losing the file silently. A row whose object is missing is visible and
// fixable; an object nobody recorded is not.

const attachmentURLExpiry = 15 * time.Minute

// FileStore is the subset of platform-go/objectstore.S3Store used here, so the
// API does not depend on the concrete type and tests can substitute it.
type FileStore interface {
	PresignPut(key string, expiry time.Duration) string
	PresignGet(key string, expiry time.Duration) string
}

type attachmentCreateRequest struct {
	OwnerType string `json:"ownerType"`
	OwnerRef  string `json:"ownerRef"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"sizeBytes"`
}

type attachmentResponse struct {
	ID        string `json:"id"`
	OwnerType string `json:"ownerType"`
	OwnerRef  string `json:"ownerRef"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"sizeBytes"`
	CreatedAt string `json:"createdAt"`
	UploadURL string `json:"uploadUrl,omitempty"`
	Pending   bool   `json:"pending"`
}

func (a *API) storageUnavailable(c *gin.Context) bool {
	if a.Files == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
			"code":    "STORAGE_UNCONFIGURED",
			"message": "object storage is not configured; set S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY",
		}})
		return true
	}
	return false
}

// PostAttachment records an attachment and returns a presigned PUT URL.
func (a *API) PostAttachment(c *gin.Context) {
	if a.storageUnavailable(c) {
		return
	}
	var req attachmentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_BODY", "message": err.Error()}})
		return
	}
	req.OwnerRef = strings.TrimSpace(req.OwnerRef)
	req.Filename = strings.TrimSpace(req.Filename)
	if req.OwnerRef == "" || req.Filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"code": "INVALID_BODY", "message": "ownerRef and filename are required"}})
		return
	}
	if req.OwnerType == "" {
		req.OwnerType = "document"
	}
	if req.Mime == "" {
		req.Mime = "application/octet-stream"
	}

	id := uuid.NewString()
	// Namespaced by service and owner so keys stay readable in the bucket and
	// two services cannot collide on one.
	key := fmt.Sprintf("finance/%s/%s/%s", req.OwnerType, req.OwnerRef, id)

	if err := a.Repo.InsertAttachment(c.Request.Context(), id, req.OwnerType,
		req.OwnerRef, req.Filename, req.Mime, key, req.SizeBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "DB_ERROR", "message": err.Error()}})
		return
	}

	c.JSON(http.StatusCreated, attachmentResponse{
		ID: id, OwnerType: req.OwnerType, OwnerRef: req.OwnerRef,
		Filename: req.Filename, Mime: req.Mime, SizeBytes: req.SizeBytes,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UploadURL: a.Files.PresignPut(key, attachmentURLExpiry),
		Pending:   true,
	})
}

// ListAttachments returns the attachments recorded against one owner reference.
func (a *API) ListAttachments(c *gin.Context) {
	ref := strings.TrimSpace(c.Query("ownerRef"))
	if ref == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"code": "INVALID_QUERY", "message": "ownerRef is required"}})
		return
	}
	list, err := a.Repo.ListAttachments(c.Request.Context(), ref)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "DB_ERROR", "message": err.Error()}})
		return
	}
	out := make([]attachmentResponse, 0, len(list))
	for _, at := range list {
		out = append(out, attachmentResponse{
			ID: at.ID, OwnerType: at.OwnerType, OwnerRef: at.OwnerRef,
			Filename: at.Filename, Mime: at.Mime, SizeBytes: at.SizeBytes,
			CreatedAt: at.CreatedAt.UTC().Format(time.RFC3339), Pending: at.Pending,
		})
	}
	c.JSON(http.StatusOK, gin.H{"attachments": out})
}

// GetAttachmentURL returns a short-lived download URL for one attachment.
func (a *API) GetAttachmentURL(c *gin.Context) {
	if a.storageUnavailable(c) {
		return
	}
	key, filename, err := a.Repo.AttachmentObject(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "attachment not found"}})
		return
	}
	if key == "" {
		// Distinguishable on purpose: the record exists but its bytes were never
		// uploaded. Reporting "not found" here would hide the backlog.
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{
			"code":    "NOT_UPLOADED",
			"message": "this attachment has no object yet; its bytes are still awaiting migration to object storage",
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"filename": filename, "url": a.Files.PresignGet(key, attachmentURLExpiry)})
}
