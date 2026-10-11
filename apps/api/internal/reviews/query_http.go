package reviews

import (
	"github.com/gin-gonic/gin"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/httpapi"
	"strconv"
)

func RegisterQueries(r *gin.Engine, s *QueryService) {
	base := "/api/repositories/:installationId/:repositoryId/pulls"
	for _, path := range []string{base, base + "/:number/runs", base + "/:number/runs/:runId"} {
		r.GET(path, func(c *gin.Context) {
			session, e := s.Auth.Session(c)
			if e != nil {
				httpapi.ServiceError(c, e)
				return
			}
			installation, e := strconv.Atoi(c.Param("installationId"))
			if e != nil {
				httpapi.Error(c, 400, "INVALID_TENANT", "安装编号无效")
				return
			}
			repository, e := strconv.Atoi(c.Param("repositoryId"))
			if e != nil {
				httpapi.Error(c, 400, "INVALID_TENANT", "仓库编号无效")
				return
			}
			tenant := contracts.TenantRef{InstallationId: installation, RepositoryId: repository}
			number, e := strconv.Atoi(c.Param("number"))
			if c.Param("number") != "" && (e != nil || number < 1) {
				httpapi.Error(c, 400, "INVALID_PR", "PR 编号无效")
				return
			}
			var out any
			if number == 0 {
				out, e = s.ListPulls(c.Request.Context(), session, tenant, c.Query("cursor"))
			} else if c.Param("runId") == "" {
				out, e = s.ListRuns(c.Request.Context(), session, tenant, number, c.Query("cursor"))
			} else {
				out, e = s.Detail(c.Request.Context(), session, tenant, number, c.Param("runId"))
			}
			if e != nil {
				httpapi.ServiceError(c, e)
				return
			}
			c.JSON(200, out)
		})
	}
}
