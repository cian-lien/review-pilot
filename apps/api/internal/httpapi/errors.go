package httpapi

import (
	"errors"
	"github.com/gin-gonic/gin"
	"reviewpilot.local/api/internal/fault"
)

func ServiceError(c *gin.Context, err error) {
	var failure *fault.Error
	if errors.As(err, &failure) {
		Error(c, failure.Status, failure.Code, "请求未完成，请检查登录、权限或刷新页面")
		return
	}
	Error(c, 503, "SERVICE_UNAVAILABLE", "服务暂不可用")
}
