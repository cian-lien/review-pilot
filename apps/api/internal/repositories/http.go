package repositories

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"strconv"
	"strings"
)

func Register(r *gin.Engine, s *Service) {
	r.GET("/api/repositories", func(c *gin.Context) {
		session, err := s.Auth.Session(c)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		repos, err := s.List(c.Request.Context(), session)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, contracts.RepositoryListResponse{Repositories: repos})
	})
	r.GET("/api/runs", func(c *gin.Context) {
		session, err := s.Auth.Session(c)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		limit := 20
		if input := c.Query("limit"); input != "" {
			value, err := strconv.Atoi(input)
			if err != nil || value < 1 || value > 20 {
				httpapi.Error(c, 400, "INVALID_LIMIT", "最多读取 20 条")
				return
			}
			limit = value
		}
		runs, err := s.Runs(c.Request.Context(), session, limit)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.JSON(200, contracts.RunListResponse{Runs: runs, AiReviewEnabled: false})
	})
	r.POST("/api/repositories/:installationId/:repositoryId", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		session, err := s.Auth.Session(c)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		if err := s.Auth.CSRF(c, session); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		installation, err := strconv.Atoi(c.Param("installationId"))
		if err != nil {
			httpapi.Error(c, 400, "INVALID_TENANT", "仓库编号无效")
			return
		}
		repo, err := strconv.Atoi(c.Param("repositoryId"))
		if err != nil {
			httpapi.Error(c, 400, "INVALID_TENANT", "仓库编号无效")
			return
		}
		var body contracts.SetRepositoryEnabledRequest
		isJSON := strings.HasPrefix(c.ContentType(), "application/json")
		if isJSON {
			data, err := io.ReadAll(c.Request.Body)
			var fields map[string]json.RawMessage
			if err != nil || json.Unmarshal(data, &fields) != nil || fields["enabled"] == nil || fields["configVersion"] == nil || string(fields["enabled"]) == "null" || string(fields["configVersion"]) == "null" {
				httpapi.Error(c, 400, "INVALID_INPUT", "参数无效")
				return
			}
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&body); err != nil {
				httpapi.Error(c, 400, "INVALID_INPUT", "参数无效")
				return
			}
		} else {
			enabled := c.PostForm("enabled")
			if enabled != "true" && enabled != "false" {
				httpapi.Error(c, 400, "INVALID_INPUT", "请刷新页面后重试")
				return
			}
			body.Enabled = enabled == "true"
			body.ConfigVersion, _ = strconv.Atoi(c.PostForm("configVersion"))
		}
		if body.ConfigVersion <= 0 || body.ConfigVersion > 9007199254740991 {
			httpapi.Error(c, 400, "INVALID_VERSION", "请刷新页面后重试")
			return
		}
		if err := s.SetEnabled(c.Request.Context(), session, contracts.TenantRef{InstallationId: installation, RepositoryId: repo}, body.Enabled, int64(body.ConfigVersion)); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		if isJSON {
			c.Status(204)
		} else {
			c.Redirect(303, s.Auth.Config.AppURL+"/repositories")
		}
	})
	r.POST("/api/repositories/sync", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		session, err := s.Auth.Session(c)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		if err := s.Auth.CSRF(c, session); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		items, err := s.Auth.Gateway.UserInstallations(c.Request.Context(), session.UserToken)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		for _, item := range items {
			if item.AppID != s.Auth.Config.GitHubAppID || item.SuspendedAt != nil {
				continue
			}
			if err := s.SyncInstallation(c.Request.Context(), session, item.ID); err != nil {
				httpapi.ServiceError(c, err)
				return
			}
		}
		c.Status(204)
	})
	r.GET("/installations/callback", func(c *gin.Context) {
		session, err := s.Auth.Session(c)
		if err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		id, err := strconv.Atoi(c.Query("installation_id"))
		if err != nil {
			httpapi.Error(c, 400, "INVALID_INSTALLATION", "安装编号无效")
			return
		}
		if err := s.SyncInstallation(c.Request.Context(), session, id); err != nil {
			httpapi.ServiceError(c, err)
			return
		}
		c.Redirect(302, s.Auth.Config.AppURL+"/repositories")
	})
}
