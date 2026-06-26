package ledger

import (
	"context"
	"errors"
	"time"

	"github.com/iag-finance/backend/internal/domain"
	"github.com/iag-finance/backend/internal/repository"
)

var ErrInvoiceNotFound = repository.ErrInvoiceNotFound

func (s *Service) GetARByDocumentRef(ctx context.Context, documentRef string) (*domain.AROpenItem, error) {
	return s.repo.GetARByDocumentRef(ctx, documentRef)
}

func (s *Service) ListARFiltered(ctx context.Context, status, q string, limit, offset int) ([]domain.AROpenItem, error) {
	return s.repo.ListAROpenItemsFiltered(ctx, status, q, limit, offset)
}

func (s *Service) UpdateARByDocumentRef(ctx context.Context, documentRef string, customerRef, description *string, dueDate *time.Time) (*domain.AROpenItem, error) {
	item, err := s.repo.UpdateAROpenItem(ctx, documentRef, customerRef, description, dueDate)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrInvoiceNotFound
	}
	return item, nil
}

func (s *Service) DeleteARByDocumentRef(ctx context.Context, documentRef string) error {
	return s.repo.DeleteAROpenItem(ctx, documentRef)
}

// --- AP (bills) legacy CRUD — mirrors the AR helpers above ------------------

func (s *Service) GetAPByDocumentRef(ctx context.Context, documentRef string) (*domain.APOpenItem, error) {
	return s.repo.GetAPByDocumentRef(ctx, documentRef)
}

func (s *Service) ListAPFiltered(ctx context.Context, status, q string, limit, offset int) ([]domain.APOpenItem, error) {
	return s.repo.ListAPOpenItemsFiltered(ctx, status, q, limit, offset)
}

func (s *Service) UpdateAPByDocumentRef(ctx context.Context, documentRef string, vendorRef, description *string, dueDate *time.Time) (*domain.APOpenItem, error) {
	item, err := s.repo.UpdateAPOpenItem(ctx, documentRef, vendorRef, description, dueDate)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrInvoiceNotFound
	}
	return item, nil
}

func (s *Service) DeleteAPByDocumentRef(ctx context.Context, documentRef string) error {
	return s.repo.DeleteAPOpenItem(ctx, documentRef)
}

func (s *Service) SalesFunnel(ctx context.Context) (repository.SalesFunnel, error) {
	return s.repo.SalesFunnel(ctx)
}

func (s *Service) ListOverdueAR(ctx context.Context, cooldownHours int) ([]repository.OverdueARItem, error) {
	return s.repo.ListOverdueAR(ctx, cooldownHours)
}

func (s *Service) MarkOverdueNotified(ctx context.Context, refs []string) error {
	return s.repo.MarkOverdueNotified(ctx, refs)
}

func (s *Service) CompleteEFRISSubmission(ctx context.Context, documentRef, status, receipt, errMsg string) error {
	return s.repo.UpdateEFRISSubmission(ctx, documentRef, status, receipt, errMsg)
}

func (s *Service) SyncBankFeed(ctx context.Context, accountCode string, from, to time.Time, lines []repository.StatementLineInput) (string, int, error) {
	stmtID, err := s.repo.ImportBankStatement(ctx, accountCode, to, 0)
	if err != nil {
		return "", 0, err
	}
	n, err := s.repo.InsertStatementLines(ctx, stmtID, lines)
	if err != nil {
		return "", 0, err
	}
	if err := s.repo.MaterializeBankTransactions(ctx, accountCode, stmtID); err != nil {
		return "", 0, err
	}
	_ = s.repo.UpdateBankStatementStatus(ctx, stmtID, "reconciling")
	return stmtID.String(), n, nil
}

func (s *Service) ListLegacyBankAccounts(ctx context.Context) ([]repository.LegacyBankAccount, error) {
	return s.repo.ListLegacyBankAccounts(ctx)
}

func (s *Service) ListLegacyBankTx(ctx context.Context, limit, offset int) ([]repository.LegacyBankTx, int, error) {
	return s.repo.ListLegacyBankTx(ctx, limit, offset)
}

func MapInvoiceStatus(item domain.AROpenItem) string {
	if item.Status == "closed" {
		return "Paid"
	}
	if item.DueDate != nil && item.DueDate.Before(time.Now().UTC()) && item.Status != "closed" {
		return "Overdue"
	}
	switch item.Status {
	case "partial":
		return "Partial"
	case "open":
		return "Open"
	default:
		return item.Status
	}
}

func ParseBalance(amount, paid string) (float64, error) {
	a, err := ParsePaymentAmount(amount)
	if err != nil {
		return 0, err
	}
	p, _ := ParsePaymentAmount(paid)
	bal := a.Sub(p)
	f, _ := bal.Float64()
	return f, nil
}

func InvoiceBalance(item domain.AROpenItem) (float64, error) {
	return ParseBalance(item.Amount, item.AmountPaid)
}

func InvoiceTotal(item domain.AROpenItem) (float64, error) {
	a, err := ParsePaymentAmount(item.Amount)
	if err != nil {
		return 0, err
	}
	f, _ := a.Float64()
	return f, nil
}

// MapBillStatus mirrors MapInvoiceStatus for AP open items.
func MapBillStatus(item domain.APOpenItem) string {
	if item.Status == "closed" {
		return "Paid"
	}
	if item.DueDate != nil && item.DueDate.Before(time.Now().UTC()) && item.Status != "closed" {
		return "Overdue"
	}
	switch item.Status {
	case "partial":
		return "Partial"
	case "open":
		return "Open"
	default:
		return item.Status
	}
}

func BillBalance(item domain.APOpenItem) (float64, error) {
	return ParseBalance(item.Amount, item.AmountPaid)
}

func BillTotal(item domain.APOpenItem) (float64, error) {
	a, err := ParsePaymentAmount(item.Amount)
	if err != nil {
		return 0, err
	}
	f, _ := a.Float64()
	return f, nil
}

func EnsureInvoice(item *domain.AROpenItem) error {
	if item == nil {
		return ErrInvoiceNotFound
	}
	return nil
}

func IsInvoiceNotFound(err error) bool {
	return errors.Is(err, ErrInvoiceNotFound) || errors.Is(err, repository.ErrOpenItemNotFound)
}
