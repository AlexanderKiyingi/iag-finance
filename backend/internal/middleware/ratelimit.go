package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ipBucket struct {
	count int
	reset time.Time
}

// GinRateLimit simple per-IP requests per minute.
func GinRateLimit(perMinute int) gin.HandlerFunc {
	if perMinute <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	var mu sync.Mutex
	buckets := map[string]*ipBucket{}
	window := time.Minute
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()
		mu.Lock()
		b, ok := buckets[ip]
		if !ok || now.After(b.reset) {
			buckets[ip] = &ipBucket{count: 1, reset: now.Add(window)}
			mu.Unlock()
			c.Next()
			return
		}
		b.count++
		if b.count > perMinute {
			mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		mu.Unlock()
		c.Next()
	}
}
