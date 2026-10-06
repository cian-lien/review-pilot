package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"time"
)

func NewRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 14*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			c.AbortWithStatus(503)
			return
		}
		c.Set("requestId", hex.EncodeToString(id))
		c.Header("X-Request-Id", c.GetString("requestId"))
		defer func() {
			if recover() != nil {
				Error(c, 500, "INTERNAL_ERROR", "请求未完成")
			}
		}()
		c.Next()
	})
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"phase": "foundation", "aiReviewEnabled": false})
	})
	r.NoRoute(func(c *gin.Context) { Error(c, 404, "NOT_FOUND", "资源不存在") })
	return r
}

func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": code, "message": message, "requestId": c.GetString("requestId")})
}
