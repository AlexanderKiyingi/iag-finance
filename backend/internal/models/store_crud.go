package models

import (
	"context"
	"strings"

	"github.com/iag/finance-backend/internal/query"
)

func (s *Store) persistUnlocked() error {
	if s.repo != nil {
		return s.repo.SaveState(context.Background(), s.snapshot())
	}
	return nil
}

func (s *Store) OpenARTotal() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return openARUnlocked(s.Invoices)
}

func (s *Store) Dashboard() DashboardPayload {
	s.mu.RLock()
	defer s.mu.RUnlock()
	up, down := true, false
	return DashboardPayload{
		OpenAR: openARUnlocked(s.Invoices),
		KPIs: []DashboardKPI{
			{Label: "Open AR balance", Value: "UGX 42.9M", Trend: "7.1% vs last week", Up: &up, Href: "/sales"},
			{Label: "Daily avg. collections", Value: "UGX 4.86M", Trend: "2% vs last week", Up: &up, Href: "/banking"},
			{Label: "URA / EFRIS compliance", Value: "92%", Trend: "1.3% vs last week", Up: &down, Href: "/taxes"},
		},
		CashFlow: CashFlowSummary{
			TotalLabel: "UGX 479M", Change: "▲ 8% vs last week",
			Periods: []string{"Last 7 days", "Last 30 days", "Last quarter", "Year to date"},
			Bars:    SeedDashBars(),
		},
		Updates: buildUpdatesUnlocked(s.Approvals, s.Audit),
	}
}

func (s *Store) ListInvoices(status, q string, page query.Page) (InvoiceListResponse, query.Page) {
	s.mu.RLock()
	all := filterInvoices(s.Invoices, status, q)
	s.mu.RUnlock()
	slice, p := query.SlicePage(all, page)
	return InvoiceListResponse{Items: slice, Meta: ListMeta{Total: p.Total, Page: p.Page, Limit: p.Limit}}, p
}

func filterInvoices(invoices []Invoice, status, q string) []Invoice {
	out := make([]Invoice, 0, len(invoices))
	q = strings.ToLower(strings.TrimSpace(q))
	for _, inv := range invoices {
		if status != "" && !strings.EqualFold(inv.Status, status) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(inv.No+inv.Customer+inv.Status), q) {
			continue
		}
		out = append(out, inv)
	}
	return out
}

func (s *Store) GetInvoice(no string) (Invoice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, inv := range s.Invoices {
		if inv.No == no {
			return inv, nil
		}
	}
	return Invoice{}, ErrNotFound
}

func (s *Store) CreateInvoice(in InvoiceInput) (Invoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	no := formatInvNo(s.nextInv)
	s.nextInv++
	inv := Invoice{No: no, Date: in.Date, Due: in.Due, Customer: in.Customer, Total: in.Total, Balance: in.Balance, Status: in.Status}
	if inv.Status == "" {
		inv.Status = "Open"
	}
	s.Invoices = append([]Invoice{inv}, s.Invoices...)
	_ = s.persistUnlocked()
	return inv, nil
}

func (s *Store) PatchInvoice(no string, patch InvoicePatch) (Invoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, inv := range s.Invoices {
		if inv.No != no {
			continue
		}
		applyInvoicePatch(&inv, patch)
		s.Invoices[i] = inv
		_ = s.persistUnlocked()
		return inv, nil
	}
	return Invoice{}, ErrNotFound
}

func applyInvoicePatch(inv *Invoice, patch InvoicePatch) {
	if patch.Date != nil {
		inv.Date = *patch.Date
	}
	if patch.Due != nil {
		inv.Due = *patch.Due
	}
	if patch.Customer != nil {
		inv.Customer = *patch.Customer
	}
	if patch.Total != nil {
		inv.Total = *patch.Total
	}
	if patch.Balance != nil {
		inv.Balance = *patch.Balance
	}
	if patch.Status != nil {
		inv.Status = *patch.Status
	}
}

func (s *Store) DeleteInvoice(no string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, inv := range s.Invoices {
		if inv.No == no {
			s.Invoices = append(s.Invoices[:i], s.Invoices[i+1:]...)
			_ = s.persistUnlocked()
			return nil
		}
	}
	return ErrNotFound
}

func (s *Store) ListBankAccounts() []BankAccount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]BankAccount(nil), s.Banks...)
}

func (s *Store) ListBankTx(page query.Page) ([]BankTx, query.Page) {
	s.mu.RLock()
	all := append([]BankTx(nil), s.BankTx...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) ListAssets(page query.Page) (AssetListResponse, query.Page) {
	s.mu.RLock()
	var cost, dep, nbv, monthly float64
	for _, a := range s.Assets {
		cost += a.Cost
		dep += a.AccumDep
		nbv += a.NBV
		if a.Useful > 0 {
			monthly += (a.Cost - a.Residual) / float64(a.Useful*12)
		}
	}
	items := append([]FixedAsset(nil), s.Assets...)
	s.mu.RUnlock()
	slice, p := query.SlicePage(items, page)
	return AssetListResponse{
		Items: slice,
		Summary: AssetSummary{GrossBook: cost, AccumDep: dep, NetBook: nbv, MonthlyDep: monthly, Count: len(items)},
		Meta:  ListMeta{Total: p.Total, Page: p.Page, Limit: p.Limit},
	}, p
}

func (s *Store) GetAsset(tag string) (FixedAsset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.Assets {
		if a.Tag == tag {
			return a, nil
		}
	}
	return FixedAsset{}, ErrNotFound
}

func (s *Store) ListApprovals(page query.Page) ([]Approval, query.Page) {
	s.mu.RLock()
	all := append([]Approval(nil), s.Approvals...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) GetApproval(id string) (Approval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.Approvals {
		if a.ID == id {
			return a, nil
		}
	}
	return Approval{}, ErrNotFound
}

type ApprovalPatch struct {
	Status *string `json:"status,omitempty"`
}

func (s *Store) PatchApproval(id string, patch ApprovalPatch) (Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.Approvals {
		if a.ID != id {
			continue
		}
		if patch.Status != nil {
			s.Approvals[i].Status = *patch.Status
		}
		_ = s.persistUnlocked()
		return s.Approvals[i], nil
	}
	return Approval{}, ErrNotFound
}

func (s *Store) ListAudit(page query.Page) ([]AuditEntry, query.Page) {
	s.mu.RLock()
	all := append([]AuditEntry(nil), s.Audit...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) AppendAuditEntry(entry AuditEntry) AuditEntry {
	s.mu.Lock()
	if entry.TS == "" {
		entry.TS = nowTS()
	}
	s.Audit = append([]AuditEntry{entry}, s.Audit...)
	s.mu.Unlock()
	s.afterMutation()
	return entry
}

func (s *Store) SalesFunnel() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var overdue, openBal, paidTotal float64
	var overdueN, openN, paidN int
	for _, inv := range s.Invoices {
		switch inv.Status {
		case "Overdue":
			overdue += inv.Balance
			overdueN++
		case "Open", "Partial":
			openBal += inv.Balance
			openN++
		case "Paid":
			paidTotal += inv.Total
			paidN++
		}
	}
	return map[string]interface{}{
		"estimate": map[string]interface{}{"value": 0, "label": "0 estimates"},
		"unbilled": map[string]interface{}{"value": 2400000, "label": "3 unbilled activity"},
		"overdue":  map[string]interface{}{"value": overdue, "count": overdueN},
		"open":     map[string]interface{}{"value": openBal, "count": openN},
		"paid":     map[string]interface{}{"value": paidTotal, "count": paidN},
	}
}

func (s *Store) ListExpenses(page query.Page) ([]Expense, query.Page) {
	s.mu.RLock()
	all := append([]Expense(nil), s.Expenses...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) ListWorkers(page query.Page) ([]Worker, query.Page) {
	s.mu.RLock()
	all := append([]Worker(nil), s.Workers...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) ListUsers(page query.Page) ([]FinanceUser, query.Page) {
	s.mu.RLock()
	all := append([]FinanceUser(nil), s.Users...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) ListNotifications(page query.Page) ([]Notification, query.Page) {
	s.mu.RLock()
	all := append([]Notification(nil), s.Notifications...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) ListBudgets(page query.Page) ([]Budget, query.Page) {
	s.mu.RLock()
	all := append([]Budget(nil), s.Budgets...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) ListJournals(page query.Page) ([]JournalEntry, query.Page) {
	s.mu.RLock()
	all := append([]JournalEntry(nil), s.Journals...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) ListInventory(page query.Page) ([]InventoryItem, query.Page) {
	s.mu.RLock()
	all := append([]InventoryItem(nil), s.Inventory...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) ListTaxes(page query.Page) ([]TaxRecord, query.Page) {
	s.mu.RLock()
	all := append([]TaxRecord(nil), s.Taxes...)
	s.mu.RUnlock()
	return query.SlicePage(all, page)
}

func (s *Store) GetSettings() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copySettings(s.Settings)
}

func (s *Store) PatchSettings(patch map[string]string) map[string]string {
	s.mu.Lock()
	for k, v := range patch {
		s.Settings[k] = v
	}
	out := copySettings(s.Settings)
	s.mu.Unlock()
	s.afterMutation()
	return out
}

func formatInvNo(n int) string {
	return "INV-" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func openARUnlocked(invoices []Invoice) float64 {
	var t float64
	for _, inv := range invoices {
		t += inv.Balance
	}
	return t
}

func buildUpdatesUnlocked(approvals []Approval, audit []AuditEntry) []FinanceUpdate {
	items := []FinanceUpdate{}
	for i, a := range approvals {
		if i >= 3 {
			break
		}
		items = append(items, FinanceUpdate{
			Icon: "ClipboardCheck", Bg: "bg-orange-500/10", Color: "text-orange-500",
			Title: a.Type + " · " + a.Status, Desc: a.Subject, Time: a.Date,
			Href: "/approvals", Period: "today",
		})
	}
	for i, a := range audit {
		if i >= 4 {
			break
		}
		period := "week"
		if i < 2 {
			period = "today"
		}
		icon := "Activity"
		if i%2 == 0 {
			icon = "FileEdit"
		}
		time := a.TS
		if parts := strings.Split(a.TS, " "); len(parts) > 1 {
			time = parts[1]
		}
		items = append(items, FinanceUpdate{
			Icon: icon, Bg: "bg-blue-500/10", Color: "text-blue-500",
			Title: a.Action, Desc: a.Entity + " · " + a.User, Time: time,
			Href: "/audit", Period: period,
		})
	}
	items = append(items,
		FinanceUpdate{Icon: "Receipt", Bg: "bg-emerald-500/10", Color: "text-emerald-500", Title: "EFRIS ack", Desc: "INV-1037 fiscalised", Time: "09:40 AM", Href: "/sales", Period: "today"},
		FinanceUpdate{Icon: "Landmark", Bg: "bg-violet-500/10", Color: "text-violet-500", Title: "Bank feed", Desc: "12 transactions to review", Time: "09:15 AM", Href: "/banking", Period: "today"},
	)
	if len(items) > 8 {
		items = items[:8]
	}
	return items
}
