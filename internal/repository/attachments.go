package repository

import (
	"context"
	"time"
)

// Attachment metadata for finance records. The bytes live in object storage;
// this is the record of what exists, who it belongs to and where to find it.
//
// The table is public.attachments, shared across services and partitioned by
// owner_service, so every query here is scoped to 'finance'. Without that scope
// a finance caller could read procurement's documents.

// Attachment is one stored file belonging to a finance record.
type Attachment struct {
	ID        string
	OwnerType string
	OwnerRef  string
	Filename  string
	Mime      string
	SizeBytes int64
	CreatedAt time.Time
	// Pending reports that no storage_key is recorded yet: the row exists but
	// its bytes are not in the bucket. The rows migrated from the monolith are
	// in this state until the backfill runs.
	Pending bool
}

// InsertAttachment records an attachment against a finance record.
func (r *Repository) InsertAttachment(ctx context.Context, id, ownerType, ownerRef, filename, mime, storageKey string, sizeBytes int64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO public.attachments
		    (id, owner_service, owner_type, owner_ref, filename, mime, size_bytes,
		     storage_key, uploaded_at, created_at)
		VALUES ($1, 'finance', $2, $3, $4, $5, $6, $7, now(), now())`,
		id, ownerType, ownerRef, filename, mime, sizeBytes, storageKey)
	return err
}

// ListAttachments returns the attachments recorded against one owner reference.
func (r *Repository) ListAttachments(ctx context.Context, ownerRef string) ([]Attachment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, owner_type, owner_ref, filename, mime, size_bytes,
		       created_at, storage_key IS NULL
		  FROM public.attachments
		 WHERE owner_service = 'finance' AND owner_ref = $1
		 ORDER BY created_at DESC`, ownerRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.OwnerType, &a.OwnerRef, &a.Filename, &a.Mime,
			&a.SizeBytes, &a.CreatedAt, &a.Pending); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AttachmentObject returns the storage key and filename for one attachment. An
// empty key means the row exists but its bytes were never uploaded, which the
// caller should report differently from "not found".
func (r *Repository) AttachmentObject(ctx context.Context, id string) (key, filename string, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT coalesce(storage_key,''), filename FROM public.attachments
		 WHERE id = $1 AND owner_service = 'finance'`, id).Scan(&key, &filename)
	return key, filename, err
}
