package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// RevaluationResult summarises a period-end FX revaluation run.
type RevaluationResult struct {
	Period      string `json:"period"`
	NetGainLoss string `json:"netGainLoss"` // base currency; positive = net gain
	Items       int    `json:"items"`
	Posted      bool   `json:"posted"`
}

// RevalueFX revalues open foreign-currency AR/AP at the period-end rate vs the
// rate each document was booked at, and posts the net unrealized gain/loss as
// `Dr 2900 FX Revaluation / Cr 7220 Unrealized FX` (net gain; swapped for a net
// loss). It also posts an auto-reversal dated the first day of the next period,
// so open items keep their historical rate for realized-FX on settlement and the
// next period revalues from a clean base. Idempotent per period (source event
// `fx.revalue:<period>`). Currencies with no period-end rate are skipped.
func (s *Service) RevalueFX(ctx context.Context, period, actor string) (*RevaluationResult, error) {
	month, err := time.Parse("2006-01", period)
	if err != nil {
		return nil, fmt.Errorf("period must be YYYY-MM")
	}
	periodEnd := month.AddDate(0, 1, -1) // last day of the month
	nextStart := month.AddDate(0, 1, 0)  // first day of next month

	balances, err := s.repo.OpenForeignBalances(ctx)
	if err != nil {
		return nil, err
	}

	rateCache := map[string]*decimal.Decimal{}
	getRate := func(ccy string) (decimal.Decimal, bool) {
		if r, ok := rateCache[ccy]; ok {
			if r == nil {
				return decimal.Zero, false
			}
			return *r, true
		}
		r, err := s.repo.GetRate(ctx, ccy, periodEnd)
		if err != nil {
			rateCache[ccy] = nil
			return decimal.Zero, false
		}
		rateCache[ccy] = &r
		return r, true
	}

	netReval := decimal.Zero
	counted := 0
	for _, b := range balances {
		rate, ok := getRate(b.Currency)
		if !ok {
			continue
		}
		delta := b.Remaining.Mul(rate.Sub(b.DocRate)).Round(2)
		if b.Direction == "ar" {
			netReval = netReval.Add(delta) // AR up = gain
		} else {
			netReval = netReval.Sub(delta) // AP up = loss
		}
		counted++
	}

	res := &RevaluationResult{Period: period, NetGainLoss: netReval.StringFixed(2), Items: counted}
	if netReval.IsZero() {
		return res, nil
	}

	reval, err := s.resolveAccount(ctx, "2900")
	if err != nil {
		return nil, err
	}
	unreal, err := s.resolveAccount(ctx, "7220")
	if err != nil {
		return nil, err
	}
	base := s.repo.BaseCurrency()
	amt := netReval.Abs()

	var fwd []repository.ResolvedLine
	if netReval.IsPositive() { // net gain → Dr 2900 / Cr 7220
		fwd = []repository.ResolvedLine{
			{AccountID: reval.ID, Debit: amt, Currency: base, LineOrder: 0, Memo: "FX revaluation"},
			{AccountID: unreal.ID, Credit: amt, Currency: base, LineOrder: 1, Memo: "Unrealized FX gain"},
		}
	} else { // net loss → Dr 7220 / Cr 2900
		fwd = []repository.ResolvedLine{
			{AccountID: unreal.ID, Debit: amt, Currency: base, LineOrder: 0, Memo: "Unrealized FX loss"},
			{AccountID: reval.ID, Credit: amt, Currency: base, LineOrder: 1, Memo: "FX revaluation"},
		}
	}
	rev := make([]repository.ResolvedLine, len(fwd))
	for i, l := range fwd {
		rev[i] = repository.ResolvedLine{
			AccountID: l.AccountID, Debit: l.Credit, Credit: l.Debit,
			Currency: base, LineOrder: l.LineOrder, Memo: "Reversal: " + l.Memo,
		}
	}

	src := "iag.finance"
	if _, err := s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description: "FX revaluation " + period, SourceService: &src,
		AccountingDate: periodEnd, Currency: base, Lines: fwd,
	}, "fx.revalue:"+period, "finance.fx.revalued", periodEnd, nil, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "fx.revalued", Message: "FX revaluation " + period,
	}); err != nil {
		return nil, err
	}
	if _, err := s.repo.BookPostedEntry(ctx, repository.CreateJournalParams{
		Description: "FX revaluation reversal " + period, SourceService: &src,
		AccountingDate: nextStart, Currency: base, Lines: rev,
	}, "fx.revalue.reversal:"+period, "finance.fx.revalued", nextStart, nil, &repository.AuditInfo{
		Actor: actorOrSystem(actor), EventType: "fx.revalued", Message: "FX revaluation reversal " + period,
	}); err != nil {
		return nil, err
	}

	res.Posted = true
	return res, nil
}
