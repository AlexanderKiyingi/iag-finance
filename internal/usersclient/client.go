// Package usersclient calls iag-users for billing identity resolution.
package usersclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	platformserviceauth "github.com/alvor-technologies/iag-platform-go/serviceauth"
)

var (
	ErrNotFound    = errors.New("billing identity not found")
	ErrForbidden   = errors.New("forbidden")
	ErrNotConfigured = errors.New("users client not configured")
)

// BillingIdentity mirrors iag-users billing identity JSON.
type BillingIdentity struct {
	ID                 string  `json:"id"`
	OrgID              string  `json:"orgId"`
	LegalName          string  `json:"legalName"`
	TaxID              *string `json:"taxId,omitempty"`
	BillingEmail       *string `json:"billingEmail,omitempty"`
	FinanceCustomerRef *string `json:"financeCustomerRef,omitempty"`
	IsPrimary          bool    `json:"isPrimary"`
}

// Client talks to iag-users (direct or via gateway).
type Client struct {
	baseURL    string
	httpClient *http.Client
	sa         *platformserviceauth.Client
}

// Config wires the users upstream.
type Config struct {
	BaseURL         string
	TokenURL        string
	ServiceClientID string
	ServiceSecret   string
}

// New returns a Client. When BaseURL is empty the client is disabled.
func New(cfg Config) *Client {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return &Client{}
	}
	var sa *platformserviceauth.Client
	if cfg.ServiceSecret != "" {
		sa = platformserviceauth.NewClient(platformserviceauth.Options{
			TokenURL:     cfg.TokenURL,
			ClientID:     cfg.ServiceClientID,
			ClientSecret: cfg.ServiceSecret,
			Audience:     "iag.users",
		})
	}
	return &Client{
		baseURL:    base,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		sa:         sa,
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) GetBillingIdentity(ctx context.Context, orgID, billingID string) (*BillingIdentity, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	path := fmt.Sprintf("/v1/orgs/%s/billing-identities/%s", orgID, billingID)
	var item BillingIdentity
	if err := c.getJSON(ctx, path, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.sa != nil {
		tok, err := c.sa.Token(ctx)
		if err != nil {
			return fmt.Errorf("users service token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("users api: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode == http.StatusForbidden {
		return ErrForbidden
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("users api %s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode users response: %w", err)
	}
	return nil
}

// DeriveCustomerRef returns financeCustomerRef or a slug from legalName.
func DeriveCustomerRef(b *BillingIdentity) string {
	if b == nil {
		return ""
	}
	if b.FinanceCustomerRef != nil && strings.TrimSpace(*b.FinanceCustomerRef) != "" {
		return strings.TrimSpace(*b.FinanceCustomerRef)
	}
	return slugLegalName(b.LegalName)
}

func slugLegalName(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
