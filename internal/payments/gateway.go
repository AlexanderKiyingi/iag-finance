// Package payments defines the provider-agnostic seam for collecting on AR open
// items. A concrete gateway (Pesapal / Flutterwave / MTN MoMo / Airtel Money)
// implements Gateway and a webhook verifier; the bundled Manual provider records
// an intent that is settled out-of-band (cash/bank transfer) via the confirm
// endpoint, reusing the existing payment path. Swapping in a real gateway is a
// matter of implementing this interface and routing its webhook.
package payments

import "context"

// IntentRequest describes a collection attempt for a gateway to initialise.
type IntentRequest struct {
	IntentID    string
	OpenItemID  string
	Amount      string
	Currency    string
	CustomerRef string
}

// IntentResult is what a gateway returns for a created intent: a provider
// reference and (for hosted-checkout providers) a URL to redirect the payer to.
type IntentResult struct {
	ExternalRef string
	CheckoutURL string
}

// Gateway is a payment-collection provider.
type Gateway interface {
	Name() string
	CreateIntent(ctx context.Context, req IntentRequest) (IntentResult, error)
}

// Manual is the built-in provider: there is no hosted checkout — the intent is
// settled out-of-band and confirmed via the API. The reference is the intent id.
type Manual struct{}

func (Manual) Name() string { return "manual" }

func (Manual) CreateIntent(_ context.Context, req IntentRequest) (IntentResult, error) {
	return IntentResult{ExternalRef: "manual:" + req.IntentID}, nil
}
