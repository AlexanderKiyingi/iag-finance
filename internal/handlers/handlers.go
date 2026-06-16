package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/iag-finance/backend/internal/chainaudit"
	"github.com/iag-finance/backend/internal/config"
	"github.com/iag-finance/backend/internal/db"
	"github.com/iag-finance/backend/internal/ledger"
	"github.com/iag-finance/backend/internal/tablerows"
	"github.com/redis/go-redis/v9"
	"github.com/alvor-technologies/iag-platform-go/apierr"
)

type Handlers struct {
	Cfg      config.Config
	DB       *db.PoolHealth
	ChainAudit *chainaudit.Store
	Tables   *tablerows.Store
	Redis    *redis.Client
}

func (h *Handlers) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"service":   h.Cfg.ServiceName,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) Ready(c *gin.Context) {
	ctx := c.Request.Context()
	if err := h.DB.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":   "not_ready",
			"service":  h.Cfg.ServiceName,
			"postgres": false,
			"error":    "database unavailable",
		})
		return
	}
	// Redis is an optional cache, not a hard dependency: a flap degrades, it
	// does not take the service out of rotation. Postgres is the only readiness
	// gate (the service degrades-don't-die on Redis — see chainaudit/auditlog).
	redisOK := true
	if h.Redis != nil {
		if err := h.Redis.Ping(ctx).Err(); err != nil {
			redisOK = false
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   "ready",
		"service":  h.Cfg.ServiceName,
		"postgres": true,
		"redis":    h.Redis != nil && redisOK,
	})
}

func (h *Handlers) AppendAudit(c *gin.Context) {
	var body chainaudit.AppendInput
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	// Actor is the authenticated principal, never client-supplied — otherwise
	// the audit trail can be attributed to anyone.
	body.Actor = chainActor(c)
	ev, err := h.ChainAudit.Append(c.Request.Context(), body)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, ev)
}

func (h *Handlers) ListAudit(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.ChainAudit.List(c.Request.Context(), limit)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": list})
}

// VerifyAudit recomputes the hash chain and reports whether it is intact.
func (h *Handlers) VerifyAudit(c *gin.Context) {
	res, err := h.ChainAudit.Verify(c.Request.Context())
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, err.Error())
		return
	}
	status := http.StatusOK
	if !res.Valid {
		status = http.StatusConflict
	}
	c.JSON(status, res)
}

func (h *Handlers) ListTableRows(c *gin.Context) {
	tableID := c.Param("tableId")
	if strings.HasPrefix(tableID, "seed_") {
		c.JSON(http.StatusGone, gin.H{
			"error":      "deprecated_table",
			"message":    "Use structured inbox APIs instead of HTML table_rows",
			"migrateTo":  seedTableMigrateHint(tableID),
		})
		return
	}
	list, err := h.Tables.List(c.Request.Context(), tableID)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"rows": list})
}

func (h *Handlers) AppendTableRow(c *gin.Context) {
	var body tablerows.AppendBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	row, err := h.Tables.Append(c.Request.Context(), c.Param("tableId"), body.RowHTML)
	if err != nil {
		apierr.JSONStatus(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (h *Handlers) ValidatePosting(c *gin.Context) {
	var body ledger.ValidateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.JSONStatus(c, http.StatusBadRequest, err.Error())
		return
	}
	res := ledger.ValidatePosting(body)
	status := http.StatusOK
	if !res.OK {
		status = http.StatusUnprocessableEntity
	}
	c.JSON(status, res)
}

func seedTableMigrateHint(tableID string) string {
	switch tableID {
	case "seed_bank_cash":
		return "/v1/inbox/bank-accounts"
	case "seed_ap_inbox":
		return "/v1/inbox/ap"
	case "seed_cherry_intake":
		return "/v1/inbox/cherry-intake"
	case "seed_coa":
		return "/v1/chart-of-accounts"
	default:
		return "/v1/chart-of-accounts"
	}
}
