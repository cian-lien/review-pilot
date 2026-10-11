package reviews

import (
	"github.com/gin-gonic/gin"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/jobs"
)

func Register(router *gin.Engine, service *Service, cfg config.Config) {
	group := jobs.ReviewGroup(router, cfg)
	group.POST("/:taskId/claim", func(c *gin.Context) {
		var req contracts.ClaimReviewRequest
		if !jobs.DecodeReviewRequest(c, "ClaimReviewRequest", &req, 65536) {
			return
		}
		if req.Envelope.TaskId != c.Param("taskId") {
			httpapi.Error(c, 409, "TASK_IDENTITY_MISMATCH", "任务身份不一致")
			return
		}
		answer, err := service.Claim(c.Request.Context(), req)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, answer)
	})
	group.POST("/:taskId/heartbeat", func(c *gin.Context) {
		var lease contracts.ReviewLease
		if !jobs.DecodeReviewRequest(c, "ReviewLease", &lease, 65536) || !jobs.CheckReviewTaskURL(c, lease) {
			return
		}
		if err := service.Heartbeat(c.Request.Context(), lease); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.Status(204)
	})
	group.POST("/:taskId/result", func(c *gin.Context) {
		var req contracts.CommitReviewRequest
		if !jobs.DecodeReviewRequest(c, "CommitReviewRequest", &req, 65536) || !jobs.CheckReviewTaskURL(c, req.Result.Lease) {
			return
		}
		answer, err := service.Commit(c.Request.Context(), req)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, answer)
	})
	group.GET("/:taskId/result", func(c *gin.Context) {
		answer, err := service.Confirm(c.Request.Context(), c.Param("taskId"), c.Query("runId"), c.Query("inputHash"))
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, answer)
	})
	for _, name := range []string{"block", "fail"} {
		group.POST("/:taskId/"+name, func(c *gin.Context) {
			var req contracts.ReviewTaskActionRequest
			if !jobs.DecodeReviewRequest(c, "ReviewTaskActionRequest", &req, 65536) || !jobs.CheckReviewTaskURL(c, req.Lease) {
				return
			}
			var err error
			if name == "block" {
				err = service.Block(c.Request.Context(), req)
			} else {
				err = service.Fail(c.Request.Context(), req)
			}
			if err != nil {
				httpapi.ServiceError(c, err)
				return
			}
			c.Status(204)
		})
	}
}
