package consumer

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	platformevents "github.com/alvor-technologies/iag-platform-go/events"

	"github.com/iag-finance/backend/internal/events"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/repository"
)

const (
	scmPartyCreated      = "scm.party.created"
	scmPartyUpdated      = "scm.party.updated"
	scmPartyPortalLinked = "scm.party.portal_linked"
	// scmFarmerPaymentRecorded is the moment a coffee purchase from a farmer
	// becomes both attributable and measurable: iag-supply-chain fixes the
	// price per kg and computes the gross. Until finance consumed it, coffee —
	// the platform's core purchase — reached the books only when iag-payments
	// settled, so between delivery and payment the ledger showed neither the
	// stock nor the money owed for it.
	scmFarmerPaymentRecorded = "scm.farmer_payment.recorded"
)

type supplyChainHandler struct {
	repo   *repository.Repository
	ledger *ledger.Service
	bus    *events.Bus
}

func (h *supplyChainHandler) Handle(ctx context.Context, env platformevents.Envelope) error {
	switch env.Type {
	case scmPartyCreated, scmPartyUpdated:
		return h.syncAPParty(ctx, env.Data)
	case scmPartyPortalLinked:
		return h.syncPortalLink(ctx, env.Data)
	case scmFarmerPaymentRecorded:
		return h.recordFarmerPayable(ctx, env)
	default:
		return nil
	}
}

// coffeePayableRef derives the finance document reference for a farmer payment.
// Stable and derived only from SCM's own business id, so a redelivery collides
// with the payable already booked instead of raising a second one — and so a
// coffee_payout settlement can find it later.
func coffeePayableRef(businessID string) string { return "CHERRY-" + businessID }

// recordFarmerPayable raises the AP item for a coffee purchase and lets the
// invoice.posted it enqueues book the journal — the same path procurement and
// fleet fuel use, so there is one way to create a payable rather than three.
//
// The whole gross capitalises: cherry is bought as stock, so it is Dr Inventory
// / Cr AP, not an expense. It becomes cost of sales when the coffee is sold.
func (h *supplyChainHandler) recordFarmerPayable(ctx context.Context, env platformevents.Envelope) error {
	if h.ledger == nil || env.Data == nil {
		return nil
	}
	businessID, _ := env.Data["business_id"].(string)
	businessID = strings.TrimSpace(businessID)
	if businessID == "" {
		return platformevents.Permanent(errMissingFarmerPaymentFields)
	}
	gross := scmAmount(env.Data["gross_ugx"])
	if !gross.IsPositive() {
		// An advance recorded before any coffee was priced owes nothing yet.
		return nil
	}
	vendorRef, _ := env.Data["party_business_id"].(string)
	vendorRef = strings.TrimSpace(vendorRef)
	if vendorRef == "" {
		return platformevents.Permanent(errMissingFarmerPaymentFields)
	}
	currency, _ := env.Data["currency"].(string)
	if strings.TrimSpace(currency) == "" {
		currency = "UGX"
	}

	documentRef := coffeePayableRef(businessID)
	desc := "Coffee purchase — " + businessID
	if batch, ok := env.Data["batch_business_id"].(string); ok && strings.TrimSpace(batch) != "" {
		desc += " (" + strings.TrimSpace(batch) + ")"
	}
	amount := gross.StringFixed(2)

	outbox := invoicePostedOutbox(h.bus, documentRef, vendorRef, amount, currency, "", "", "", "", false)
	if outbox != nil {
		// Capitalise rather than expense: this is stock arriving, and there is
		// no purchase order behind a cherry delivery to route it through GR/IR.
		outbox.Payload["inventoryValue"] = amount
	}
	item, err := h.ledger.CreateAPItem(ctx, vendorRef, documentRef, desc, amount, currency, nil, outbox)
	if err != nil {
		if repository.IsUniqueViolation(err) {
			slog.Debug("finance coffee payable already exists", "documentRef", documentRef)
			return nil
		}
		return err
	}
	slog.Info("finance AP item from farmer payment", "documentRef", documentRef, "id", item.ID, "gross", amount)
	return nil
}

var errMissingFarmerPaymentFields = errors.New("scm.farmer_payment.recorded missing business_id or party_business_id")

// scmAmount coerces an SCM money field, which arrives as a JSON number for the
// integer UGX columns and occasionally as a string.
func scmAmount(v any) decimal.Decimal {
	switch x := v.(type) {
	case float64:
		return decimal.NewFromFloat(x)
	case string:
		d, err := decimal.NewFromString(strings.TrimSpace(x))
		if err != nil {
			return decimal.Zero
		}
		return d
	default:
		return decimal.Zero
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
	// Also land the SCM party in finance's vendor master so it shows up in the
	// vendor list (not just as a backfilled party_id on AP rows). Only vendor-like
	// suppliers become finance vendors; farmers/cooperatives are AP counterparties
	// but not billing vendors, so they stop at the AP backfill above.
	if strings.EqualFold(strings.TrimSpace(supplierType), "vendor") {
		if err := h.repo.UpsertVendorByParty(ctx, repository.DefaultEntityID, partyID,
			businessID, name, "", "", "", "Active"); err != nil {
			return err
		}
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
// It also lands SCM vendor parties in the finance vendor master, and raises the
// AP payable for a recorded coffee purchase.
//
// ledgerSvc may be nil, in which case farmer payables are ignored rather than
// booked — the party sync does not need it.
func NewSupplyChain(cfg Config, repo *repository.Repository, ledgerSvc *ledger.Service, bus *events.Bus, dlq *platformevents.Producer) (*Consumer, error) {
	h := &supplyChainHandler{repo: repo, ledger: ledgerSvc, bus: bus}
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
