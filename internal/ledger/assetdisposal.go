package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

const (
	disposalCashAccount  = "1000" // cash / bank
	fixedAssetsAccount   = "1500" // fixed assets (gross cost)
	accumDepAccount      = "1510" // accumulated depreciation (contra-asset)
	gainOnDisposalAcct   = "4200" // gain on asset disposal (other income)
	lossOnDisposalAcct   = "5200" // loss on asset disposal (expense)
)

// BookAssetDisposal books the GL effect of a stores asset disposal. When the
// asset is capitalised in the fixed-asset subledger, it de-recognises the system
// cost and accumulated depreciation and marks the asset disposed (in one tx):
//
//	Dr Cash                     proceeds
//	Dr Accumulated Depreciation accumulated
//	Cr Fixed Assets             cost
//	Cr/Dr Gain/Loss             proceeds − NBV   (NBV = cost − accumulated)
//
// When the asset is NOT in the subledger it falls back to the hand-entered book
// value carried on the event: Dr Cash / Cr Fixed Assets(book value) / gain-loss,
// with proceeds recognised entirely as a gain if no book value was given. A
// scrap/write-off with neither proceeds nor any value books nothing. Idempotent
// on eventID; the period-close check and base-currency conversion ride the
// shared booking path.
func (s *Service) BookAssetDisposal(ctx context.Context, eventID, eventType, source, correlationID, currency, assetTag, method string, proceeds, bookValue decimal.Decimal) (*domain.JournalEntry, error) {
	if proceeds.IsNegative() {
		proceeds = decimal.Zero
	}
	if bookValue.IsNegative() {
		bookValue = decimal.Zero
	}

	if currency == "" {
		currency = s.repo.BaseCurrency()
	}
	// Posting date is now; refuse a closed period before either path books.
	if closed, err := s.repo.IsPeriodClosed(ctx, time.Now().UTC().Format("2006-01")); err != nil {
		return nil, err
	} else if closed {
		return nil, ErrPeriodClosed
	}

	desc := fmt.Sprintf("Asset disposal %s (%s)", assetTag, method)

	// Registered path: de-recognise system cost + accumulated depreciation and
	// mark the asset disposed atomically (the fa_asset row is locked FOR UPDATE
	// inside the booking tx, so a concurrent depreciation run cannot make the
	// accumulated value stale).
	entry, handled, err := s.repo.BookAssetDisposalSubledger(ctx, eventID, eventType, source, correlationID, currency, assetTag, desc, proceeds, &repository.AuditInfo{
		Actor:     "system:" + source,
		EventType: "ledger.asset.disposed",
		Message:   desc,
	})
	if err != nil {
		return nil, err
	}
	if handled {
		return entry, nil
	}

	// Fallback: not capitalised in the subledger — use the carried book value.
	lines := make([]LineInput, 0, 3)
	if proceeds.IsPositive() {
		lines = append(lines, LineInput{AccountCode: disposalCashAccount, Debit: proceeds, Memo: "Disposal proceeds " + assetTag})
	}
	if bookValue.IsPositive() {
		lines = append(lines, LineInput{AccountCode: fixedAssetsAccount, Credit: bookValue, Memo: "Asset de-recognition " + assetTag})
	}
	net := proceeds.Sub(bookValue)
	switch {
	case net.IsPositive():
		lines = append(lines, LineInput{AccountCode: gainOnDisposalAcct, Credit: net, Memo: "Gain on disposal"})
	case net.IsNegative():
		lines = append(lines, LineInput{AccountCode: lossOnDisposalAcct, Debit: net.Neg(), Memo: "Loss on disposal"})
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return s.BookFromEvent(ctx, eventID, eventType, source, correlationID, desc, currency, lines)
}
