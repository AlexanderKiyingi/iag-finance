package consumer

// The event types finance consumes, declared in one place.
//
// This is the seam between the code and docs/EVENT_CONTRACT.md, which is the
// document other teams integrate against. It had drifted: six of the types
// below were being consumed and booked with no mention in the contract at all,
// including erp.payroll.run_posted — which books an entire payroll journal —
// and the whole iag.payments subscription. An integrator reading the contract
// would not have known finance already acted on their events.
//
// TestEventContractDocumentsEveryHandledType keeps the two in step. Adding a
// case to a handler's switch means adding it here, which then means writing the
// documentation row — the same trick warehouseMovementTypes plays in the ledger
// package, applied to the contract instead of the postings.
type Subscription struct {
	// Group is the Kafka consumer group, which is what makes each stream
	// advance and replay independently.
	Group string
	// Producers names the services that emit these types, for the doc row.
	Producers string
	Types     []string
}

// Subscriptions lists every event type finance dispatches on, by consumer group.
func Subscriptions() []Subscription {
	return []Subscription{
		{
			Group:     "iag.finance.ledger",
			Producers: "iag-finance (and any service publishing to iag.finance)",
			Types:     []string{"sale.completed", "invoice.posted"},
		},
		{
			Group:     "iag.finance.fleet",
			Producers: "iag-fleet",
			Types:     []string{"fleet.fuel.recorded"},
		},
		{
			Group:     "iag.finance.payments",
			Producers: "iag-payments",
			Types:     []string{"payments.settled"},
		},
		{
			Group:     "iag.finance.supply-chain",
			Producers: "iag-supply-chain",
			Types: []string{
				scmPartyCreated, scmPartyUpdated, scmPartyPortalLinked,
				scmFarmerPaymentRecorded,
			},
		},
		{
			Group:     "iag.finance.commercial",
			Producers: "iag-procurement, iag-contract-management",
			Types:     []string{procurementInvoiceReceived, procurementGrnPosted, contractsPaymentAuthorized},
		},
		{
			Group:     "iag.finance.vendor-sync",
			Producers: "iag-procurement, iag-supply-chain",
			Types:     []string{vendorUpserted},
		},
		{
			Group:     "iag.finance.erp",
			Producers: "iag-erp",
			Types: []string{
				erpEmployeeCreated, erpEmployeeUpdated, erpEmployeeTerminated,
				erpLeaveApproved, erpLeaveRejected, erpLeaveCancelled,
				erpLeaveBalanceChanged, erpEmployeeRateChanged,
				erpPayrollRunPosted,
			},
		},
		{
			Group:     "iag.finance.warehouse",
			Producers: "iag-warehouse",
			Types:     []string{warehouseAssetDisposed, warehouseMovementPosted},
		},
	}
}

// HandledTypes flattens Subscriptions to the bare list of event types.
func HandledTypes() []string {
	var out []string
	for _, s := range Subscriptions() {
		out = append(out, s.Types...)
	}
	return out
}
