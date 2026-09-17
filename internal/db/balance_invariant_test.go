package db

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The balance trigger has been redefined three times (018 nominal, 028 base,
// 069 nominal again by accident). Whichever migration defines it *last* is the
// one production runs, so this pins that definition to the base-currency
// comparison. A future migration that redefines the function from the 018
// text fails here instead of rejecting every FX entry in production.
func TestLastBalanceTriggerComparesBaseAmounts(t *testing.T) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	fn := regexp.MustCompile(`(?s)CREATE OR REPLACE FUNCTION assert_journal_balanced\(\).*?\$\$ LANGUAGE plpgsql;`)
	var last, lastFile string
	for _, name := range names {
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if m := fn.FindString(string(body)); m != "" {
			last, lastFile = m, name
		}
	}
	if last == "" {
		t.Fatal("no migration defines assert_journal_balanced")
	}
	if !strings.Contains(last, "SUM(debit_base)") || !strings.Contains(last, "SUM(credit_base)") {
		t.Fatalf("%s defines assert_journal_balanced on nominal amounts; the ledger invariant is SUM(debit_base) = SUM(credit_base) (see 028 and 077)", lastFile)
	}
}
