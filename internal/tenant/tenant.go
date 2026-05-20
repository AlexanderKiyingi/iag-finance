package tenant

import "github.com/gin-gonic/gin"

const Header = "X-IAG-Tenant"

func FromGin(c *gin.Context) string {
	if t := c.GetHeader(Header); t != "" {
		return t
	}
	return "default"
}
