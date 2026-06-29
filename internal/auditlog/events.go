package auditlog

// HTTP activity (middleware).
const EventHTTPRequest = "http.request"

// Business events.
const (
	EventChartAccountCreated     = "chart_of_account.created"
	EventChartAccountUpdated     = "chart_of_account.updated"
	EventChartAccountDeactivated = "chart_of_account.deactivated"
	EventJournalCreated          = "journal_entry.created"
	EventJournalPosted           = "journal_entry.posted"
	EventJournalBookedEvent      = "journal_entry.booked_from_event"
	EventARPayment               = "ar_open_item.payment_applied"
	EventAPPayment               = "ap_open_item.payment_applied"
	EventFiscalPeriodClosed      = "fiscal_period.closed"
	EventFiscalPeriodReopened    = "fiscal_period.reopened"
	EventPayrollRunPosted        = "payroll_run.posted"
)
