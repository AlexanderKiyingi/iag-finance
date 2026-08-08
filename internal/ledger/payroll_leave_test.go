package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/iag-finance/backend/internal/repository"
)

// The leave provision books a signed movement, so the direction is where it can
// go wrong: getting it backwards would relieve an obligation that is growing.
// These cover the line shape without a ledger or a database.

func leaveProvisionLines(amount decimal.Decimal) []LineInput {
	amt := amount.Abs()
	memo := "Accrued leave 2026-08"
	if amount.IsPositive() {
		return []LineInput{
			{AccountCode: acctSalaryExpense, Debit: amt, Memo: memo},
			{AccountCode: acctAccruedLeave, Credit: amt, Memo: memo},
		}
	}
	return []LineInput{
		{AccountCode: acctAccruedLeave, Debit: amt, Memo: memo},
		{AccountCode: acctSalaryExpense, Credit: amt, Memo: memo},
	}
}

func netByAccount(lines []LineInput) map[string]decimal.Decimal {
	out := map[string]decimal.Decimal{}
	for _, l := range lines {
		out[l.AccountCode] = out[l.AccountCode].Add(l.Debit).Sub(l.Credit)
	}
	return out
}

func TestLeaveProvisionIncreaseRaisesTheLiability(t *testing.T) {
	amt := decimal.NewFromInt(4_000_000)
	net := netByAccount(leaveProvisionLines(amt))

	// A liability grows on the credit side, so its net reads negative here.
	if got := net[acctAccruedLeave]; !got.Equal(amt.Neg()) {
		t.Errorf("accrued leave net %s, want a %s credit — a growing obligation", got, amt)
	}
	if got := net[acctSalaryExpense]; !got.Equal(amt) {
		t.Errorf("salary expense net %s, want a %s debit", got, amt)
	}
}

func TestLeaveProvisionDecreaseRelievesTheLiability(t *testing.T) {
	amt := decimal.NewFromInt(1_500_000)
	net := netByAccount(leaveProvisionLines(amt.Neg()))

	if got := net[acctAccruedLeave]; !got.Equal(amt) {
		t.Errorf("accrued leave net %s, want a %s debit — the obligation shrinking", got, amt)
	}
	if got := net[acctSalaryExpense]; !got.Equal(amt.Neg()) {
		t.Errorf("salary expense net %s, want a %s credit", got, amt.Neg())
	}
}

// Booking a movement one period and its exact reversal the next must leave the
// liability where it started. This is the invariant that says the signs agree.
func TestLeaveProvisionMovementsNetToZero(t *testing.T) {
	amt := decimal.NewFromInt(987_654)
	net := decimal.Zero
	for _, a := range []decimal.Decimal{amt, amt.Neg()} {
		for _, l := range leaveProvisionLines(a) {
			if l.AccountCode == acctAccruedLeave {
				net = net.Add(l.Debit).Sub(l.Credit)
			}
		}
	}
	if !net.IsZero() {
		t.Fatalf("accrued leave nets to %s across a movement and its reversal, want zero", net)
	}
}

func TestLeaveProvisionBalances(t *testing.T) {
	for _, amt := range []decimal.Decimal{
		decimal.NewFromInt(1),
		decimal.NewFromInt(-1),
		decimal.RequireFromString("1234.56"),
		decimal.RequireFromString("-1234.56"),
	} {
		debit, credit := decimal.Zero, decimal.Zero
		for _, l := range leaveProvisionLines(amt) {
			debit = debit.Add(l.Debit)
			credit = credit.Add(l.Credit)
		}
		if !debit.Equal(credit) {
			t.Errorf("amount %s: debits %s != credits %s", amt, debit, credit)
		}
	}
}

// The expense side is the account payroll already uses, so staff cost stays on
// one line rather than being split across two for no reporting benefit.
func TestLeaveProvisionExpensesToTheSameAccountAsPayroll(t *testing.T) {
	if acctSalaryExpense != "5200" {
		t.Fatalf("salary expense account is %s; the provision assumes payroll's own account", acctSalaryExpense)
	}
	for _, l := range leaveProvisionLines(decimal.NewFromInt(100)) {
		if l.AccountCode != acctSalaryExpense && l.AccountCode != acctAccruedLeave {
			t.Errorf("provision touched %s; it should only move between salary expense and accrued leave", l.AccountCode)
		}
	}
}

// A date is valued once. Re-running one now answers from the row that was
// booked instead of measuring again — the ledger would refuse a second entry
// for the same date anyway, and the old code turned that refusal into a 500.
// The rebuild parses numerics that arrive as text, so it is worth pinning.

func TestValuationRebuildKeepsWhatWasBooked(t *testing.T) {
	id := uuid.New()
	row := &repository.LeaveValuationRow{
		ValuedAt:         time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
		TotalLiability:   "1250.75",
		Movement:         "-99.25",
		EmployeesValued:  12,
		EmployeesUnrated: 3,
		JournalEntryID:   &id,
	}
	got, err := leaveValuationFromRow(row)
	if err != nil {
		t.Fatalf("rebuilding a booked valuation: %v", err)
	}
	if got.TotalLiability.String() != "1250.75" {
		t.Errorf("total = %s, want 1250.75", got.TotalLiability)
	}
	// The sign carries the direction of the posting; losing it would report a
	// release of the liability as a further accrual.
	if got.Movement.String() != "-99.25" {
		t.Errorf("movement = %s, want -99.25", got.Movement)
	}
	if got.JournalEntryID == nil || *got.JournalEntryID != id {
		t.Error("the journal that was posted is not carried back to the caller")
	}
	if got.EmployeesUnrated != 3 {
		t.Errorf("unrated employees = %d, want 3", got.EmployeesUnrated)
	}
	if !got.ValuedAt.Equal(row.ValuedAt) {
		t.Errorf("valuedAt = %s, want %s", got.ValuedAt, row.ValuedAt)
	}
}

// A valuation with no journal is a real state: a period whose measured total
// did not move is recorded so it stays distinguishable from one nobody valued.
func TestValuationRebuildAllowsAPeriodThatBookedNothing(t *testing.T) {
	got, err := leaveValuationFromRow(&repository.LeaveValuationRow{
		TotalLiability: "500", Movement: "0",
	})
	if err != nil {
		t.Fatalf("rebuilding an unposted valuation: %v", err)
	}
	if got.JournalEntryID != nil {
		t.Error("a valuation that posted nothing should carry no journal id")
	}
	if !got.Movement.IsZero() {
		t.Errorf("movement = %s, want zero", got.Movement)
	}
}

// An unreadable figure must surface. Collapsing it to zero would report a
// liability of nothing and book the whole balance again on the next run.
func TestValuationRebuildRefusesAnUnreadableFigure(t *testing.T) {
	if _, err := leaveValuationFromRow(&repository.LeaveValuationRow{
		TotalLiability: "1,250.75", Movement: "0",
	}); err == nil {
		t.Error("an unparseable total was accepted; it must be reported")
	}
	if _, err := leaveValuationFromRow(&repository.LeaveValuationRow{
		TotalLiability: "500", Movement: "n/a",
	}); err == nil {
		t.Error("an unparseable movement was accepted; it must be reported")
	}
}
