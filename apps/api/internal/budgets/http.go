package budgets

import (
	"github.com/gin-gonic/gin"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/jobs"
)

func Register(router *gin.Engine, service *Service, cfg config.Config) {
	group := jobs.ReviewGroup(router, cfg)
	group.POST("/:taskId/model-calls/reserve", func(c *gin.Context) {
		var req contracts.ReserveModelCallRequest
		if !jobs.DecodeReviewRequest(c, "ReserveModelCallRequest", &req, 65536) || !jobs.CheckReviewTaskURL(c, req.Lease) {
			return
		}
		call, err := service.Reserve(c.Request.Context(), req)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, call)
	})
	group.POST("/:taskId/model-calls/:callId/dispatch", func(c *gin.Context) {
		var req contracts.DispatchModelCallRequest
		if !jobs.DecodeReviewRequest(c, "DispatchModelCallRequest", &req, 65536) || !jobs.CheckReviewTaskURL(c, req.Lease) {
			return
		}
		if req.CallId != c.Param("callId") {
			httpapi.Error(c, 409, "MODEL_CALL_IDENTITY_MISMATCH", "调用身份不一致")
			return
		}
		call, err := service.Dispatch(c.Request.Context(), req)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, call)
	})
	group.POST("/:taskId/model-calls/:callId/settle", func(c *gin.Context) {
		var req contracts.SettleModelCallRequest
		if !jobs.DecodeReviewRequest(c, "SettleModelCallRequest", &req, 65536) || !jobs.CheckReviewTaskURL(c, req.Lease) {
			return
		}
		if req.CallId != c.Param("callId") {
			httpapi.Error(c, 409, "MODEL_CALL_IDENTITY_MISMATCH", "调用身份不一致")
			return
		}
		call, err := service.Settle(c.Request.Context(), req)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, call)
	})
}
