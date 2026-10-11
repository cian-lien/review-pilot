package reviewcontext

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/jobs"
)

func Register(router *gin.Engine, service *Service, cfg config.Config) {
	group := jobs.ReviewGroup(router, cfg)
	group.POST("/:taskId/context", func(c *gin.Context) {
		var req contracts.ReadReviewContextRequest
		if !jobs.DecodeReviewRequest(c, "ReadReviewContextRequest", &req, 65536) || !jobs.CheckReviewTaskURL(c, req.Lease) {
			return
		}
		page, err := service.Read(c.Request.Context(), req)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		body, err := json.Marshal(page)
		if err != nil || len(body) > service.Policy.MaxPageBytes {
			httpapi.Error(c, 503, "CONTEXT_TOO_LARGE", "上下文超出限额")
			return
		}
		c.Data(200, "application/json", body)
	})
}
