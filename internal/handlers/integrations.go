package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Status string

const (
	StatusStub Status = "stub"
)

type Health struct {
	Name        string    `json:"name"`
	Status      Status    `json:"status"`
	Description string    `json:"description"`
	CheckedAt   time.Time `json:"checkedAt"`
}

func (h *Handlers) URAStatus(c *gin.Context) {
	now := time.Now().UTC()
	c.JSON(http.StatusOK, Health{
		Name:        "ura-efris",
		Status:      StatusStub,
		Description: "URA EFRIS e-invoicing — adapter not configured (Phase 1 stub)",
		CheckedAt:   now,
	})
}

func (h *Handlers) BankingStatus(c *gin.Context) {
	now := time.Now().UTC()
	c.JSON(http.StatusOK, Health{
		Name:        "banking",
		Status:      StatusStub,
		Description: "Bank reconciliation — adapter not configured (Phase 1 stub)",
		CheckedAt:   now,
	})
}
