package jobs

import (
	"crypto/subtle"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"strings"
)

func Register(router *gin.Engine, service *Service, cfg config.Config) {
	group := router.Group("/internal/tasks")
	group.Use(func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if len(cfg.ReviewWorkerServiceToken) < 32 || cfg.ReviewWorkerServiceToken == cfg.EvolutionWorkerServiceToken {
			httpapi.Error(c, 503, "SERVICE_IDENTITY_UNAVAILABLE", "服务身份尚未配置")
			return
		}
		if len(cfg.EvolutionWorkerServiceToken) >= 32 && subtle.ConstantTimeCompare([]byte(token), []byte(cfg.EvolutionWorkerServiceToken)) == 1 {
			httpapi.Error(c, 403, "SERVICE_ROLE_FORBIDDEN", "S1 不允许学习任务")
			return
		}
		if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.ReviewWorkerServiceToken)) != 1 {
			httpapi.Error(c, 401, "SERVICE_UNAUTHORIZED", "服务身份无效")
			return
		}
		if !uuidPattern.MatchString(c.Param("taskId")) {
			httpapi.Error(c, 400, "INVALID_TASK_ID", "任务标识无效")
			return
		}
		c.Next()
	})
	group.POST("/:taskId/claim", func(c *gin.Context) {
		var request contracts.ClaimRequest
		if !decode(c, &request) {
			return
		}
		if request.Envelope.TaskId != c.Param("taskId") {
			httpapi.Error(c, 409, "TASK_IDENTITY_MISMATCH", "任务身份不一致")
			return
		}
		result, err := service.Claim(c.Request.Context(), request.Envelope, request.WorkerId)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, result)
	})
	group.POST("/:taskId/heartbeat", func(c *gin.Context) {
		var lease contracts.TaskLease
		if !decode(c, &lease) {
			return
		}
		if lease.TaskId != c.Param("taskId") {
			httpapi.Error(c, 409, "TASK_IDENTITY_MISMATCH", "任务身份不一致")
			return
		}
		if err := service.Heartbeat(c.Request.Context(), lease); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.Status(204)
	})
	group.POST("/:taskId/connection-result", func(c *gin.Context) {
		var result contracts.ConnectionResult
		if !decode(c, &result) {
			return
		}
		if result.TaskId != c.Param("taskId") {
			httpapi.Error(c, 409, "TASK_IDENTITY_MISMATCH", "任务身份不一致")
			return
		}
		answer, err := service.CommitConnection(c.Request.Context(), result)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, answer)
	})
	group.GET("/:taskId/result", func(c *gin.Context) {
		result, err := service.ConfirmResult(c.Request.Context(), c.Param("taskId"), c.Query("runId"), c.Query("inputHash"), c.Query("resultHash"))
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, result)
	})
	group.POST("/:taskId/fail", func(c *gin.Context) {
		var lease contracts.TaskLease
		if !decode(c, &lease) {
			return
		}
		if lease.TaskId != c.Param("taskId") {
			httpapi.Error(c, 409, "TASK_IDENTITY_MISMATCH", "任务身份不一致")
			return
		}
		if err := service.Fail(c.Request.Context(), lease, "CONNECTION_JOB_FAILED"); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.Status(204)
	})
}
func decode(c *gin.Context, dest any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		httpapi.Error(c, 400, "INVALID_REQUEST", "请求格式无效")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		httpapi.Error(c, 400, "INVALID_REQUEST", "请求格式无效")
		return false
	}
	return true
}
