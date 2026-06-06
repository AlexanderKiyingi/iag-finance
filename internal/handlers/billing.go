package handlers

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/iag-finance/backend/internal/usersclient"
)

type billingResolveInput struct {
	OrgID             string
	BillingIdentityID string
	CustomerRef       string
}

type billingResolveResult struct {
	CustomerRef       string
	BillingOrgID      *uuid.UUID
	BillingIdentityID *uuid.UUID
}

func (a *API) resolveBillingCustomerRef(ctx context.Context, in billingResolveInput) (billingResolveResult, error) {
	orgID := strings.TrimSpace(in.OrgID)
	billingID := strings.TrimSpace(in.BillingIdentityID)
	explicit := strings.TrimSpace(in.CustomerRef)

	if billingID != "" && orgID == "" {
		return billingResolveResult{}, errors.New("orgId is required when billingIdentityId is set")
	}
	if billingID == "" && explicit == "" {
		return billingResolveResult{}, errors.New("customerRef or (orgId + billingIdentityId) is required")
	}

	if billingID == "" {
		return billingResolveResult{CustomerRef: explicit}, nil
	}

	if a.Users == nil || !a.Users.Enabled() {
		return billingResolveResult{}, errors.New("billing identity resolution requires USERS_API_URL and SERVICE_CLIENT_SECRET")
	}

	bi, err := a.Users.GetBillingIdentity(ctx, orgID, billingID)
	if err != nil {
		if errors.Is(err, usersclient.ErrNotFound) {
			return billingResolveResult{}, errors.New("billing identity not found")
		}
		if errors.Is(err, usersclient.ErrForbidden) {
			return billingResolveResult{}, errors.New("cannot read billing identity")
		}
		return billingResolveResult{}, err
	}

	resolved := usersclient.DeriveCustomerRef(bi)
	if resolved == "" {
		return billingResolveResult{}, errors.New("billing identity has no legalName or financeCustomerRef")
	}
	if explicit != "" && !strings.EqualFold(explicit, resolved) {
		return billingResolveResult{}, errors.New("customerRef does not match billing identity financeCustomerRef")
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return billingResolveResult{}, errors.New("invalid orgId")
	}
	billUUID, err := uuid.Parse(billingID)
	if err != nil {
		return billingResolveResult{}, errors.New("invalid billingIdentityId")
	}

	return billingResolveResult{
		CustomerRef:       resolved,
		BillingOrgID:      &orgUUID,
		BillingIdentityID: &billUUID,
	}, nil
}
