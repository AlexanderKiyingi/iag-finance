package consumer

import (
	"context"
	"strings"

	"github.com/google/uuid"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"

	"github.com/iag-finance/backend/internal/repository"
)

// vendorUpserted is the canonical cross-service vendor master event. Procurement
// (and, in future, other services) emit it; finance ingests it to keep its own
// vendor party master in step, keyed on the shared party_id. Upsert is naturally
// idempotent, so platform-go NoopDedupe is sufficient — no processed_events row.
const vendorUpserted = "party.vendor.upserted"

// financeSource is finance's own CloudEvents source; events stamped with it are
// skipped so finance never re-ingests what it emitted (mesh loop guard).
const financeSource = "iag.finance"

type vendorSyncHandler struct {
	repo *repository.Repository
}

func (h *vendorSyncHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	if env.Type != vendorUpserted {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(env.Source), financeSource) {
		return nil
	}
	if env.Data == nil {
		return nil
	}
	partyRaw, _ := env.Data["party_id"].(string)
	partyRaw = strings.TrimSpace(partyRaw)
	if partyRaw == "" {
		return nil
	}
	partyID, err := uuid.Parse(partyRaw)
	if err != nil {
		return platformevents.Permanent(err)
	}
	code, _ := env.Data["code"].(string)
	name, _ := env.Data["name"].(string)
	email, _ := env.Data["email"].(string)
	phone, _ := env.Data["phone"].(string)
	currency, _ := env.Data["currency"].(string)
	status, _ := env.Data["status"].(string)

	return h.repo.UpsertVendorByParty(ctx, repository.DefaultEntityID, partyID,
		code, name, email, phone, currency, status)
}

// NewVendorSync builds a consumer that ingests party.vendor.upserted into the
// finance vendor master.
func NewVendorSync(cfg Config, repo *repository.Repository, dlq *platformevents.Producer) (*Consumer, error) {
	h := &vendorSyncHandler{repo: repo}
	inner, err := platformevents.NewConsumer(platformevents.ConsumerConfig{
		Brokers:     cfg.Brokers,
		Topic:       cfg.Topic,
		GroupID:     cfg.GroupID,
		Handler:     h,
		Dedupe:      platformevents.NoopDedupe{},
		DLQProducer: dlq,
		DLQTopic:    cfg.DLQTopic,
	})
	if err != nil {
		return nil, err
	}
	return &Consumer{inner: inner}, nil
}
