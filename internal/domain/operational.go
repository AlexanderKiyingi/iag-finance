package domain

import (
	"time"

	"github.com/google/uuid"
)

type BankAccount struct {
	ID            uuid.UUID `json:"id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	Institution   string    `json:"institution"`
	Currency      string    `json:"currency"`
	Balance       string    `json:"balance"`
	StatusLabel   string    `json:"statusLabel"`
	Purpose       string    `json:"purpose"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type CherryIntakeLine struct {
	ID           uuid.UUID `json:"id"`
	IntakeCode   string    `json:"intakeCode"`
	FarmerName   string    `json:"farmerName"`
	QtyKg        string    `json:"qtyKg"`
	AmountUgx    string    `json:"amountUgx"`
	StatusLabel  string    `json:"statusLabel"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
