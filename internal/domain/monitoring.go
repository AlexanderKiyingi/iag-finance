package domain

import "time"

type MonitoringSummary struct {
	Service           string         `json:"service"`
	CheckedAt         time.Time      `json:"checkedAt"`
	ChartOfAccounts   int            `json:"chartOfAccounts"`
	JournalDraft      int            `json:"journalEntriesDraft"`
	JournalPosted     int            `json:"journalEntriesPosted"`
	AROpenItems       int            `json:"arOpenItems"`
	APOpenItems       int            `json:"apOpenItems"`
	ProcessedEvents   int            `json:"processedEvents"`
	AuditLast24Hours  int            `json:"auditEventsLast24Hours"`
	AuditLastHour     int            `json:"auditEventsLastHour"`
	HTTPErrorsLast24h int            `json:"httpErrorsLast24Hours"`
	Integrations      []Integration  `json:"integrations"`
}

type Integration struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
