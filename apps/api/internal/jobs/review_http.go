package jobs

import (
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"strings"
)

func ReviewGroup(router *gin.Engine, cfg config.Config) *gin.RouterGroup {
	group := router.Group("/internal/review-tasks")
	group.Use(func(c *gin.Context) {
		if len(cfg.ReviewWorkerServiceToken) < 32 || cfg.ReviewWorkerServiceToken == cfg.EvolutionWorkerServiceToken {
			httpapi.Error(c, 503, "SERVICE_IDENTITY_UNAVAILABLE", "服务身份尚未配置")
			return
		}
		header := c.GetHeader("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if len(cfg.EvolutionWorkerServiceToken) >= 32 && subtle.ConstantTimeCompare([]byte(token), []byte(cfg.EvolutionWorkerServiceToken)) == 1 {
			httpapi.Error(c, 403, "SERVICE_ROLE_FORBIDDEN", "审查任务只允许 review Worker")
			return
		}
		if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.ReviewWorkerServiceToken)) != 1 {
			httpapi.Error(c, 401, "SERVICE_UNAUTHORIZED", "服务身份无效")
			return
		}
		if !uuidPattern.MatchString(c.Param("taskId")) {
			httpapi.Error(c, 400, "INVALID_TASK_ID", "任务标识无效")
			return
		}
		c.Next()
	})
	return group
}

func DecodeReviewRequest(c *gin.Context, name string, dest any, limit int) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(limit))
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || contracts.DecodeReview(name, body, dest, limit) != nil {
		httpapi.Error(c, 400, "INVALID_REVIEW_REQUEST", "审查请求格式无效")
		return false
	}
	return true
}
func CheckReviewTaskURL(c *gin.Context, l contracts.ReviewLease) bool {
	if c.Param("taskId") != l.TaskId {
		httpapi.Error(c, 409, "TASK_IDENTITY_MISMATCH", "任务身份不一致")
		return false
	}
	return true
}
