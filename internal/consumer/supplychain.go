package consumer

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"

	"github.com/iag-finance/backend/internal/repository"
)

const (
	scmPartyCreated = "scm.party.created"
	scmPartyUpdated = "scm.party.updated"
)

type supplyChainHandler struct {
	repo *repository.Repository
}

func (h *supplyChainHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	switch env.Type {
	case scmPartyCreated, scmPartyUpdated:
	default:
		return nil
	}
	data := env.Data
	if data == nil {
		return nil
	}
	supplierType, _ := data["supplier_type"].(string)
	switch strings.ToLower(strings.TrimSpace(supplierType)) {
	case "vendor", "cooperative", "farmer":
	default:
		return nil
	}
	partyRaw, _ := data["party_id"].(string)
	partyRaw = strings.TrimSpace(partyRaw)
	if partyRaw == "" {
		return nil
	}
	partyID, err := uuid.Parse(partyRaw)
	if err != nil {
		return platformevents.Permanent(err)
	}
	businessID, _ := data["party_business_id"].(string)
	businessID = strings.TrimSpace(businessID)
	if businessID == "" {
		businessID = partyRaw
	}
	name, _ := data["name"].(string)
	n, err := h.repo.SyncAPPartyFromSCM(ctx, partyID, businessID, name)
	if err != nil {
		return err
	}
	if n > 0 {
		slog.Info("finance party sync updated AP rows", "party_id", partyID, "rows", n)
	}
	return nil
}

// NewSupplyChain builds a consumer for iag.supply-chain party sync (Phase 4.6).
func NewSupplyChain(cfg Config, repo *repository.Repository, dlq *platformevents.Producer) (*Consumer, error) {
	h := &supplyChainHandler{repo: repo}
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
