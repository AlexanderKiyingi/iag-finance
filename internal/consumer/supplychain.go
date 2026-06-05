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
	scmPartyCreated      = "scm.party.created"
	scmPartyUpdated      = "scm.party.updated"
	scmPartyPortalLinked = "scm.party.portal_linked"
)

type supplyChainHandler struct {
	repo *repository.Repository
}

func (h *supplyChainHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	switch env.Type {
	case scmPartyCreated, scmPartyUpdated:
		return h.syncAPParty(ctx, env.Data)
	case scmPartyPortalLinked:
		return h.syncPortalLink(ctx, env.Data)
	default:
		return nil
	}
}

func (h *supplyChainHandler) syncAPParty(ctx context.Context, data map[string]any) error {
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

func (h *supplyChainHandler) syncPortalLink(ctx context.Context, data map[string]any) error {
	if data == nil {
		return nil
	}
	userRaw, _ := data["platform_user_id"].(string)
	partyRaw, _ := data["party_id"].(string)
	userRaw = strings.TrimSpace(userRaw)
	partyRaw = strings.TrimSpace(partyRaw)
	if userRaw == "" || partyRaw == "" {
		return nil
	}
	platformUserID, err := uuid.Parse(userRaw)
	if err != nil {
		return platformevents.Permanent(err)
	}
	partyID, err := uuid.Parse(partyRaw)
	if err != nil {
		return platformevents.Permanent(err)
	}
	businessID, _ := data["party_business_id"].(string)
	supplierType, _ := data["supplier_type"].(string)
	return h.repo.UpsertPortalPartyLink(ctx, platformUserID, partyID, businessID, supplierType)
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
