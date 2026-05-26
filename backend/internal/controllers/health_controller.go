package controllers

import (
	"context"
	"net/http"
	"time"

	"github.com/iag/finance-backend/internal/models"
	"github.com/iag/finance-backend/internal/views"
)

type HealthController struct{}

func NewHealthController() *HealthController {
	return &HealthController{}
}

func (c *HealthController) Check(w http.ResponseWriter, r *http.Request) {
	views.JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "iag-finance-api"})
}

func (c *HealthController) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{"api": "ok"}
	status := http.StatusOK
	if models.HasPostgres() {
		if err := models.PostgresReady(ctx); err != nil {
			checks["postgres"] = err.Error()
			status = http.StatusServiceUnavailable
		} else {
			checks["postgres"] = "ok"
		}
	} else {
		checks["postgres"] = "skipped"
	}
	if models.HasRedis() {
		if err := models.RedisReady(ctx); err != nil {
			checks["redis"] = err.Error()
			status = http.StatusServiceUnavailable
		} else {
			checks["redis"] = "ok"
		}
	} else {
		checks["redis"] = "skipped"
	}
	views.JSON(w, status, map[string]any{"status": checks})
}
