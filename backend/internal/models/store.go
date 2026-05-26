package models

import (
	"context"
	"strings"
	"sync"
	"time"
)

// PersistedState is the full application snapshot saved to file or Postgres JSONB.
type PersistedState struct {
	Session       Session           `json:"session"`
	Invoices      []Invoice         `json:"invoices"`
	BankAccounts  []BankAccount     `json:"bankAccounts"`
	BankTx        []BankTx          `json:"bankTransactions"`
	FixedAssets   []FixedAsset      `json:"fixedAssets"`
	Approvals     []Approval        `json:"approvals"`
	AuditLog      []AuditEntry      `json:"auditLog"`
	Expenses      []Expense         `json:"expenses"`
	Workers       []Worker          `json:"workers"`
	Users         []FinanceUser     `json:"users"`
	Notifications []Notification    `json:"notifications"`
	Budgets       []Budget          `json:"budgets"`
	Journals      []JournalEntry    `json:"journals"`
	Inventory     []InventoryItem   `json:"inventory"`
	Taxes         []TaxRecord       `json:"taxes"`
	Settings      map[string]string `json:"settings"`
	NextInv       int               `json:"nextInv"`
}

type StoreOptions struct {
	Repo  StateRepo
	Tokens TokenStore
	JWT   TokenIssuer
}

type Store struct {
	mu       sync.RWMutex
	dataPath string
	repo     StateRepo
	tokens   TokenStore
	jwt      TokenIssuer

	Session       Session
	Company       Company
	Invoices      []Invoice
	Banks         []BankAccount
	BankTx        []BankTx
	Assets        []FixedAsset
	Approvals     []Approval
	Audit         []AuditEntry
	Expenses      []Expense
	Workers       []Worker
	Users         []FinanceUser
	Notifications []Notification
	Budgets       []Budget
	Journals      []JournalEntry
	Inventory     []InventoryItem
	Taxes         []TaxRecord
	Settings      map[string]string
	nextInv       int
}

type StoreDeps struct {
	pg    interface{ Ping(context.Context) error }
	redis interface{ Ping(context.Context) error }
}

var deps StoreDeps

func SetHealthDeps(pg, redis interface{}) {
	deps.pg = nil
	deps.redis = nil
	if p, ok := pg.(interface{ Ping(context.Context) error }); ok {
		deps.pg = p
	}
	if r, ok := redis.(interface{ Ping(context.Context) error }); ok {
		deps.redis = r
	}
}

func HasPostgres() bool { return deps.pg != nil }
func HasRedis() bool    { return deps.redis != nil }

func PostgresReady(ctx context.Context) error {
	if deps.pg == nil {
		return nil
	}
	return deps.pg.Ping(ctx)
}

func RedisReady(ctx context.Context) error {
	if deps.redis == nil {
		return nil
	}
	return deps.redis.Ping(ctx)
}

func NewStore(dataPath string, opts *StoreOptions) *Store {
	s := &Store{dataPath: dataPath, Company: SeedCompany()}
	if opts != nil {
		s.repo = opts.Repo
		s.tokens = opts.Tokens
		s.jwt = opts.JWT
	}
	s.applyState(NewPersistedState())
	if s.repo != nil {
		ctx := context.Background()
		if st, err := s.repo.LoadState(ctx); err == nil {
			s.applyState(st)
		} else if pg, ok := s.repo.(interface {
			SeedStateIfEmpty(context.Context, *PersistedState) error
		}); ok {
			_ = pg.SeedStateIfEmpty(ctx, NewPersistedState())
			if st2, err2 := s.repo.LoadState(ctx); err2 == nil {
				s.applyState(st2)
			}
		}
	}
	return s
}

func (s *Store) applyState(p *PersistedState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Session = p.Session
	s.Invoices = p.Invoices
	s.Banks = p.BankAccounts
	s.BankTx = p.BankTx
	s.Assets = p.FixedAssets
	s.Approvals = p.Approvals
	s.Audit = p.AuditLog
	s.Expenses = p.Expenses
	s.Workers = p.Workers
	s.Users = p.Users
	s.Notifications = p.Notifications
	s.Budgets = p.Budgets
	s.Journals = p.Journals
	s.Inventory = p.Inventory
	s.Taxes = p.Taxes
	s.Settings = p.Settings
	if s.Settings == nil {
		s.Settings = DefaultSettings()
	}
	s.nextInv = p.NextInv
	if s.nextInv == 0 {
		s.nextInv = 1043
	}
}

func (s *Store) snapshot() *PersistedState {
	return &PersistedState{
		Session: s.Session, Invoices: s.Invoices, BankAccounts: s.Banks, BankTx: s.BankTx,
		FixedAssets: s.Assets, Approvals: s.Approvals, AuditLog: s.Audit,
		Expenses: s.Expenses, Workers: s.Workers, Users: s.Users,
		Notifications: s.Notifications, Budgets: s.Budgets, Journals: s.Journals,
		Inventory: s.Inventory, Taxes: s.Taxes, Settings: s.Settings, NextInv: s.nextInv,
	}
}

func (s *Store) afterMutation() {
	s.mu.RLock()
	repo := s.repo
	snap := s.snapshot()
	s.mu.RUnlock()
	if repo != nil {
		_ = repo.SaveState(context.Background(), snap)
	}
}

func (s *Store) DataPath() string { return s.dataPath }

func (s *Store) JWTEnabled() bool { return s.jwt != nil }

func (s *Store) SetSession(sess Session) {
	s.mu.Lock()
	s.Session = sess
	s.mu.Unlock()
}

func (s *Store) Reset() {
	s.applyState(NewPersistedState())
	s.afterMutation()
}

func (s *Store) GetSession() Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Session
}

func (s *Store) Bootstrap() BootstrapResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bootstrapLocked()
}

func (s *Store) bootstrapLocked() BootstrapResponse {
	return BootstrapResponse{
		Session: s.Session, Company: s.Company,
		Invoices: append([]Invoice(nil), s.Invoices...),
		BankAccounts: append([]BankAccount(nil), s.Banks...),
		BankTx: append([]BankTx(nil), s.BankTx...),
		FixedAssets: append([]FixedAsset(nil), s.Assets...),
		Approvals: append([]Approval(nil), s.Approvals...),
		AuditLog: append([]AuditEntry(nil), s.Audit...),
		Expenses: append([]Expense(nil), s.Expenses...),
		Workers: append([]Worker(nil), s.Workers...),
		Users: append([]FinanceUser(nil), s.Users...),
		Notifications: append([]Notification(nil), s.Notifications...),
		Budgets: append([]Budget(nil), s.Budgets...),
		Journals: append([]JournalEntry(nil), s.Journals...),
		Inventory: append([]InventoryItem(nil), s.Inventory...),
		Taxes: append([]TaxRecord(nil), s.Taxes...),
		Settings: copySettings(s.Settings),
		Permissions: PermissionContextFor(s.Session.Role),
	}
}

func copySettings(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Store) PatchSession(patch SessionPatch) (Session, error) {
	s.mu.Lock()
	if patch.DisplayName != nil {
		s.Session.DisplayName = strings.TrimSpace(*patch.DisplayName)
	}
	if patch.Entity != nil {
		s.Session.Entity = strings.TrimSpace(*patch.Entity)
	}
	sess := s.Session
	s.mu.Unlock()
	s.afterMutation()
	return sess, nil
}

func (s *Store) Logout() {
	s.mu.Lock()
	s.Session = DefaultSession()
	s.mu.Unlock()
	s.afterMutation()
}

func nowTS() string {
	return time.Now().Format("2006-01-02 15:04")
}

// Invoice, banking, assets, dashboard methods continue in store_crud.go
