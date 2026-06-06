package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iag-finance/backend/internal/config"
)

// EFRISAdapter submits fiscal documents to URA EFRIS.
type EFRISAdapter interface {
	Mode() string
	Ping(ctx context.Context) error
	Submit(ctx context.Context, req EFRISSubmitRequest) (EFRISSubmitResult, error)
}

type EFRISSubmitRequest struct {
	DocumentRef string
	Amount      string
	Currency    string
	CustomerRef string
}

type EFRISSubmitResult struct {
	Status      string // submitted, acknowledged, failed
	URAReceipt  string
	ErrorMessage string
}

type stubEFRIS struct {
	simulate bool
}

func (s *stubEFRIS) Mode() string {
	if s.simulate {
		return "simulate"
	}
	return "stub"
}

func (s *stubEFRIS) Ping(context.Context) error { return nil }

func (s *stubEFRIS) Submit(_ context.Context, req EFRISSubmitRequest) (EFRISSubmitResult, error) {
	if s.simulate {
		return EFRISSubmitResult{
			Status:     "acknowledged",
			URAReceipt: "SIM-" + req.DocumentRef,
		}, nil
	}
	return EFRISSubmitResult{}, fmt.Errorf("URA EFRIS not configured: set URA_EFRIS_BASE_URL")
}

type httpEFRIS struct {
	baseURL    string
	apiKey     string
	tin        string
	httpClient *http.Client
}

func (h *httpEFRIS) Mode() string { return "http" }

func (h *httpEFRIS) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(h.baseURL, "/")+"/health", nil)
	if err != nil {
		return err
	}
	h.applyHeaders(req)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("efris health %s", resp.Status)
	}
	return nil
}

func (h *httpEFRIS) Submit(ctx context.Context, in EFRISSubmitRequest) (EFRISSubmitResult, error) {
	payload := map[string]any{
		"documentRef": in.DocumentRef,
		"amount":      in.Amount,
		"currency":    in.Currency,
		"customerRef": in.CustomerRef,
		"tin":         h.tin,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.baseURL, "/")+"/v1/invoices/fiscalise", bytes.NewReader(body))
	if err != nil {
		return EFRISSubmitResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	h.applyHeaders(req)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return EFRISSubmitResult{Status: "failed", ErrorMessage: err.Error()}, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return EFRISSubmitResult{Status: "failed", ErrorMessage: strings.TrimSpace(string(raw))}, nil
	}
	var out struct {
		Receipt string `json:"receipt"`
		Status  string `json:"status"`
	}
	_ = json.Unmarshal(raw, &out)
	status := out.Status
	if status == "" {
		status = "acknowledged"
	}
	return EFRISSubmitResult{Status: status, URAReceipt: out.Receipt}, nil
}

func (h *httpEFRIS) applyHeaders(req *http.Request) {
	if h.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.apiKey)
	}
	req.Header.Set("X-TIN", h.tin)
}

func newEFRISAdapter(cfg config.Config) EFRISAdapter {
	mode := strings.ToLower(strings.TrimSpace(cfg.EFRISMode))
	if cfg.EFRISSimulate || mode == "simulate" {
		return &stubEFRIS{simulate: true}
	}
	if mode == "ura_s2s" || (mode == "" && cfg.EFRISS2SURL != "" && cfg.EFRISBaseURL == "") {
		return newURAS2SEFRIS(cfg)
	}
	if cfg.EFRISBaseURL != "" || mode == "http" {
		return &httpEFRIS{
			baseURL: cfg.EFRISBaseURL,
			apiKey:  cfg.EFRISAPIKey,
			tin:     cfg.EFRISTIN,
			httpClient: &http.Client{Timeout: 30 * time.Second},
		}
	}
	return &stubEFRIS{}
}
