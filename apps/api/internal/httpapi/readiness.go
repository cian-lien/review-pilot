package httpapi

import (
	"context"
	"github.com/gin-gonic/gin"
	"time"
)

func RegisterReadiness(r *gin.Engine, check func(context.Context) error) {
	r.GET("/api/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if check == nil || check(ctx) != nil {
			Error(c, 503, "NOT_READY", "服务尚未就绪")
			return
		}
		c.Status(204)
	})
}
