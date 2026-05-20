package ledger

import (
	"math"
	"regexp"
)

var openPeriodRE = regexp.MustCompile(`^2026-(04|05)$`)

type ValidateBody struct {
	Debit  float64 `json:"debit"`
	Credit float64 `json:"credit"`
	Period string  `json:"period"`
	Role   string  `json:"role"`
}

type ValidateResult struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues,omitempty"`
}

// ValidatePosting mirrors the browser posting simulator in iag-finance.html (validatePostingSimulator).
func ValidatePosting(b ValidateBody) ValidateResult {
	canPost := b.Role == "CFO" || b.Role == "Controller"
	openPeriod := openPeriodRE.MatchString(b.Period)
	balanced := math.Abs(b.Debit-b.Credit) < 0.01 && b.Debit > 0

	var issues []string
	if !balanced {
		issues = append(issues, "Debit and credit totals do not balance")
	}
	if !openPeriod {
		issues = append(issues, "Posting period is closed or invalid")
	}
	if !canPost {
		issues = append(issues, b.Role+" can draft but cannot post")
	}
	return ValidateResult{OK: len(issues) == 0, Issues: issues}
}
