package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Guards against one column list gaining two readers.
//
// This package has already produced that defect once: payroll_runs was read by
// three separate hand-written scans, employer_nssf was added to the SELECTs and
// to only one of them, and two were left reading thirteen columns into twelve
// destinations. Scan takes ...any, so nothing failed at build time — the
// mismatch surfaced only when the query ran.
//
// Forty-three wide scans in this package predate that fix, ten of them
// duplicating a shape. Rewriting all of them is a separate piece of work, so
// this test is a ratchet rather than a clean assertion: the known duplicates
// are listed below, nothing may be added to the list, and anything fixed must
// be removed from it. The list can only shrink.

// wideScan is the number of destinations above which a Scan is reading a whole
// entity rather than a leaf table, and is therefore worth de-duplicating.
const wideScan = 8

// knownDuplicateShapes are the scan shapes that were already duplicated when
// this test was written, keyed by "file firstDestination" with the number of
// copies. They are debt, not permission.
//
// To fix one: extract a single scanner the way payroll_runs.go does, then
// delete the entry. The test fails if an entry is stale, so the list cannot
// silently over-state the debt.
var knownDuplicateShapes = map[string]int{
	"adjustments.go &a.ID":    4,
	"income_tax.go &d.ID":     2,
	"invoices.go &i.ID":       2,
	"payments.go &i.ID":       2,
	"payments.go &p.ID":       2,
	"reconciliation.go &l.ID": 3,
	"repository.go &a.ID":     4,
	"repository.go &e.ID":     3,
	"repository.go &i.ID":     2,
	"repository.go &l.ID":     2,
}

var (
	scanCallSiteRe = regexp.MustCompile(`(?s)(rows|row)\.Scan\((.*?)\)\s*;?\s*(err|;|\n)`)
	columnConstRe  = regexp.MustCompile("(?s)const (\\w+)Columns = `([^`]*)`")
	scanFuncRe     = regexp.MustCompile(`(?s)func scan(\w+)\(row pgx\.Row[^)]*\) \(\*\w+, error\) \{(.*?)\n\}`)
	scanInnerRe    = regexp.MustCompile(`(?s)row\.Scan\((.*?)\)\s*;`)
)

// countDestinations counts `&x` arguments at paren depth zero, so an & inside a
// nested call is not counted as a destination.
func countDestinations(args string) int {
	depth, count := 0, 0
	for i, r := range args {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case '&':
			if depth == 0 && (i == 0 || strings.ContainsRune(" \t\n,", rune(args[i-1]))) {
				count++
			}
		}
	}
	return count
}

// countColumns counts comma-separated columns at paren depth zero, so a comma
// inside a subquery or an IN list is not mistaken for a separator.
func countColumns(list string) int {
	if strings.TrimSpace(list) == "" {
		return 0
	}
	depth, count := 0, 1
	for _, r := range list {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}

func packageSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(body)
	}
	if len(out) == 0 {
		t.Fatal("no package sources found")
	}
	return out
}

// duplicateShapes returns every "file firstDestination" read by more than one
// wide scan, with its count.
func duplicateShapes(t *testing.T) map[string]int {
	t.Helper()
	counts := map[string]int{}
	wide := 0
	for file, src := range packageSources(t) {
		for _, m := range scanCallSiteRe.FindAllStringSubmatch(src, -1) {
			if countDestinations(m[2]) < wideScan {
				continue
			}
			wide++
			first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m[2]), ",", 2)[0])
			counts[file+" "+first]++
		}
	}
	if wide == 0 {
		t.Fatal("found no wide scans at all; the pattern has drifted from the code " +
			"and this test is no longer checking anything")
	}
	dups := map[string]int{}
	for k, n := range counts {
		if n > 1 {
			dups[k] = n
		}
	}
	return dups
}

func TestNoNewDuplicateScanShapes(t *testing.T) {
	found := duplicateShapes(t)

	var added, grown, stale []string
	for shape, n := range found {
		baseline, known := knownDuplicateShapes[shape]
		switch {
		case !known:
			added = append(added, shape)
		case n > baseline:
			grown = append(grown, shape)
		}
	}
	for shape, baseline := range knownDuplicateShapes {
		if n, still := found[shape]; !still || n < baseline {
			stale = append(stale, shape)
		}
	}
	sort.Strings(added)
	sort.Strings(grown)
	sort.Strings(stale)

	for _, s := range added {
		t.Errorf("%s: %d hand-written scans of one shape. Extract a single scanner "+
			"(see payroll_runs.go) — this is the defect that put employer_nssf into "+
			"the wrong column", s, found[s])
	}
	for _, s := range grown {
		t.Errorf("%s: copies grew from %d to %d", s, knownDuplicateShapes[s], found[s])
	}
	for _, s := range stale {
		t.Errorf("%s is listed in knownDuplicateShapes but is no longer duplicated "+
			"at that count — remove or lower the entry so the list keeps shrinking", s)
	}
}

// Where the one-column-list-one-reader convention is followed, the two must
// agree on how many columns there are.
func TestColumnListsMatchTheirScanners(t *testing.T) {
	sources := packageSources(t)

	columns := map[string]int{}
	scanners := map[string]int{}
	files := map[string]string{}

	for file, src := range sources {
		for _, m := range columnConstRe.FindAllStringSubmatch(src, -1) {
			name := strings.ToUpper(m[1][:1]) + m[1][1:]
			columns[name] = countColumns(m[2])
		}
		for _, m := range scanFuncRe.FindAllStringSubmatch(src, -1) {
			inner := scanInnerRe.FindStringSubmatch(m[2])
			if inner == nil {
				continue
			}
			scanners[m[1]] = countDestinations(inner[1])
			files[m[1]] = file
		}
	}

	checked := 0
	for name, cols := range columns {
		dests, ok := scanners[name]
		if !ok {
			continue
		}
		checked++
		if cols != dests {
			t.Errorf("%s: %sColumns selects %d columns but scan%s passes %d destinations (%s)",
				name, strings.ToLower(name[:1])+name[1:], cols, name, dests, files[name])
		}
	}
	if checked == 0 {
		t.Fatal("matched no column list to a scanner; the convention has moved and " +
			"this test is vacuous")
	}
	t.Logf("checked %d column list/scanner pairs", checked)
}
