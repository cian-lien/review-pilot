package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"strings"
)

const MaxBody = 2 << 20

func Register(router *gin.Engine, service *Service, cfg config.Config) {
	router.POST("/api/github/webhook", func(c *gin.Context) {
		if cfg.GitHubWebhookSecret == "" {
			httpapi.Error(c, 503, "WEBHOOK_NOT_CONFIGURED", "Webhook 尚未配置")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, MaxBody))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				httpapi.Error(c, 413, "BODY_TOO_LARGE", "请求体过大")
			} else {
				httpapi.Error(c, 400, "BODY_INVALID", "请求体无效")
			}
			return
		}
		signature := c.GetHeader("X-Hub-Signature-256")
		actual, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
		mac := hmac.New(sha256.New, []byte(cfg.GitHubWebhookSecret))
		mac.Write(raw)
		if err != nil || !strings.HasPrefix(signature, "sha256=") || len(signature) != 71 || !hmac.Equal(actual, mac.Sum(nil)) {
			httpapi.Error(c, 401, "SIGNATURE_INVALID", "签名无效")
			return
		}
		accepted, err := service.Accept(c.Request.Context(), c.GetHeader("X-GitHub-Delivery"), c.GetHeader("X-GitHub-Event"), raw)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(202, contracts.WebhookResponse{Accepted: accepted})
	})
}
