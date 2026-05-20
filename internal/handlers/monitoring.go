package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (a *API) MonitoringSummary(c *gin.Context) {
	summary, err := a.Audit.MonitoringSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load monitoring summary"})
		return
	}
	if a.ConsumerEnabled {
		for i := range summary.Integrations {
			if summary.Integrations[i].Name == "kafka-consumer" {
				summary.Integrations[i].Status = "enabled"
			}
		}
	}
	c.JSON(http.StatusOK, summary)
}

func (a *API) MonitoringActivity(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
	items, err := a.Audit.RecentActivity(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load activity"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) MonitoringLedger(c *gin.Context) {
	summary, err := a.Audit.MonitoringSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load ledger stats"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"chartOfAccounts": summary.ChartOfAccounts,
		"journal": gin.H{
			"draft":  summary.JournalDraft,
			"posted": summary.JournalPosted,
		},
		"arOpenItems":     summary.AROpenItems,
		"apOpenItems":     summary.APOpenItems,
		"processedEvents": summary.ProcessedEvents,
	})
}
