package reviewstages

import (
	"github.com/gin-gonic/gin"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/jobs"
)

func Register(router *gin.Engine, service *Service, cfg config.Config) {
	group := jobs.ReviewGroup(router, cfg)
	group.POST("/:taskId/stages/read", func(c *gin.Context) {
		var req contracts.ReadReviewStageRequest
		if !jobs.DecodeReviewRequest(c, "ReadReviewStageRequest", &req, 65536) || !jobs.CheckReviewTaskURL(c, req.Lease) {
			return
		}
		answer, err := service.Read(c.Request.Context(), req)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, answer)
	})
	group.POST("/:taskId/stages", func(c *gin.Context) {
		var req contracts.SaveReviewStageRequest
		if !jobs.DecodeReviewRequest(c, "SaveReviewStageRequest", &req, 262144) || !jobs.CheckReviewTaskURL(c, req.Lease) {
			return
		}
		if err := service.Save(c.Request.Context(), req); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, contracts.CommitResponse{Outcome: contracts.Accepted})
	})
}
