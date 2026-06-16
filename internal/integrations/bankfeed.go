package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iag-finance/backend/internal/config"
)

// BankFeedAdapter pulls statement lines from a bank feed API.
type BankFeedAdapter interface {
	Mode() string
	Ping(ctx context.Context) error
	FetchLines(ctx context.Context, accountCode string, from, to time.Time) ([]BankFeedLine, error)
}

type BankFeedLine struct {
	Date        time.Time
	Description string
	Payee       string
	Amount      string
	Direction   string // credit | debit
	ExternalRef string
}

type stubBankFeed struct {
	simulate bool
}

func (s *stubBankFeed) Mode() string {
	if s.simulate {
		return "simulate"
	}
	return "stub"
}

func (s *stubBankFeed) Ping(context.Context) error { return nil }

func (s *stubBankFeed) FetchLines(_ context.Context, _ string, _, _ time.Time) ([]BankFeedLine, error) {
	return nil, fmt.Errorf("bank feed not configured: set BANK_FEED_BASE_URL")
}

type httpBankFeed struct {
	baseURL    string
	apiKey     string
	provider   string
	httpClient *http.Client
}

func (h *httpBankFeed) Mode() string { return "http" }

func (h *httpBankFeed) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(h.baseURL, "/")+"/health", nil)
	if err != nil {
		return err
	}
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("bank feed health %s", resp.Status)
	}
	return nil
}

func (h *httpBankFeed) FetchLines(ctx context.Context, accountCode string, from, to time.Time) ([]BankFeedLine, error) {
	url := fmt.Sprintf("%s/v1/accounts/%s/statements?from=%s&to=%s",
		strings.TrimRight(h.baseURL, "/"),
		accountCode,
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}
	if h.provider != "" {
		req.Header.Set("X-Bank-Provider", h.provider)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bank feed %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var payload struct {
		Lines []struct {
			Date        string `json:"date"`
			Description string `json:"description"`
			Payee       string `json:"payee"`
			Amount      string `json:"amount"`
			Direction   string `json:"direction"`
			ExternalRef string `json:"externalRef"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	out := make([]BankFeedLine, 0, len(payload.Lines))
	for _, l := range payload.Lines {
		d, err := time.Parse("2006-01-02", l.Date)
		if err != nil {
			continue
		}
		// Drop lines with an unparseable direction rather than silently
		// defaulting to "debit" (which would post money-out for malformed data).
		dir := strings.ToLower(strings.TrimSpace(l.Direction))
		if dir != "credit" && dir != "debit" {
			continue
		}
		out = append(out, BankFeedLine{
			Date: d, Description: l.Description, Payee: l.Payee,
			Amount: l.Amount, Direction: dir, ExternalRef: l.ExternalRef,
		})
	}
	return out, nil
}

func newBankFeedAdapter(cfg config.Config) BankFeedAdapter {
	if cfg.BankFeedSimulate {
		return &simulateBankFeed{}
	}
	if cfg.BankFeedBaseURL != "" {
		return &httpBankFeed{
			baseURL:    cfg.BankFeedBaseURL,
			apiKey:     cfg.BankFeedAPIKey,
			provider:   cfg.BankFeedProvider,
			httpClient: &http.Client{Timeout: 45 * time.Second},
		}
	}
	return &stubBankFeed{}
}

// simulateBankFeed returns demo lines when BANK_FEED_SIMULATE=true (local dev).
type simulateBankFeed struct{}

func (s *simulateBankFeed) Mode() string { return "simulate" }

func (s *simulateBankFeed) Ping(context.Context) error { return nil }

func (s *simulateBankFeed) FetchLines(_ context.Context, _ string, from, _ time.Time) ([]BankFeedLine, error) {
	return []BankFeedLine{
		{Date: from, Description: "SIM WIRE IN", Payee: "Demo Customer", Amount: "1000000", Direction: "credit", ExternalRef: "SIM-CR-1"},
		{Date: from, Description: "SIM PAYMENT OUT", Payee: "Demo Vendor", Amount: "250000", Direction: "debit", ExternalRef: "SIM-DB-1"},
	}, nil
}
