package ledger

import (
	"regexp"

	"github.com/shopspring/decimal"
)

// periodRE matches a well-formed accounting period 'YYYY-MM'. (The previous
// implementation hard-coded ^2026-(04|05)$, which silently rejected every other
// month.) Authoritative period-close enforcement lives in PostJournalEntry,
// which checks fiscal_periods in the database; this is a UI pre-flight check.
var periodRE = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

type ValidateBody struct {
	Debit  string `json:"debit"`
	Credit string `json:"credit"`
	Period string `json:"period"`
	Role   string `json:"role"`
}

type ValidateResult struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues,omitempty"`
}

// ValidatePosting is a pre-flight simulator for the posting UI. It uses decimal
// money (not float) and validates the period format; it does not replace the
// server-side balance constraint and fiscal-period checks enforced on a real post.
func ValidatePosting(b ValidateBody) ValidateResult {
	var issues []string

	debit, errD := decimal.NewFromString(b.Debit)
	credit, errC := decimal.NewFromString(b.Credit)
	switch {
	case errD != nil || errC != nil:
		issues = append(issues, "Debit and credit must be valid amounts")
	case !debit.Equal(credit) || debit.LessThanOrEqual(decimal.Zero):
		issues = append(issues, "Debit and credit totals do not balance")
	}

	if !periodRE.MatchString(b.Period) {
		issues = append(issues, "Posting period must be a valid 'YYYY-MM'")
	}

	if b.Role != "CFO" && b.Role != "Controller" {
		issues = append(issues, b.Role+" can draft but cannot post")
	}

	return ValidateResult{OK: len(issues) == 0, Issues: issues}
}
