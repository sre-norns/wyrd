package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	e "github.com/sre-norns/wyrd/identity/model"
)

func systemQuery(ctx *gin.Context) (e.SystemQuery, error) {
	q := e.SystemQuery{Sort: ctx.Query("sort"), Direction: ctx.Query("direction"), Search: ctx.Query("q"), Status: ctx.Query("status"), OwnerSetup: ctx.Query("owner_setup"), LimitState: ctx.Query("limit_state"), Cursor: ctx.Query("cursor"), AccountID: e.AccountID(ctx.Query("account_id")), Kind: ctx.Query("kind"), Action: ctx.Query("action"), Outcome: ctx.Query("outcome"), ActorID: ctx.Query("actor_id"), RequestID: ctx.Query("request_id")}
	invalid := &server.Problem{Status: 400, Code: "invalid-query", Detail: "Use a limit from 1 to 100 and RFC3339 time filters."}
	if value := ctx.Query("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 {
			return q, invalid
		}
		q.Limit = n
	}
	for key, dest := range map[string]**time.Time{"from": &q.From, "till": &q.Till} {
		if value := ctx.Query(key); value != "" {
			t, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return q, invalid
			}
			*dest = &t
		}
	}
	return q, nil
}

func accountUpdate(srv *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1048577))
		if err != nil || len(body) > 1048576 {
			writeProblem(ctx, &server.Problem{Status: 413, Code: "body-too-large", Detail: "The request exceeds 1 MiB."})
			return
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(body, &fields); err != nil {
			writeProblem(ctx, &server.Problem{Status: 400, Code: "invalid-json", Detail: "Invalid JSON body."})
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewReader(body))
		if _, action := fields["operation"]; action {
			ResourceValueAPI[e.SystemAction]()(ctx)
			if ctx.IsAborted() {
				return
			}
			result, err := srv.ChangeAccountLifecycle(ctx.Request.Context(), e.AccountID(ctx.Param("id")), RequireResource[e.SystemAction](ctx))
			response[e.SystemAccount](ctx).Found(result, true, err)
		} else {
			ResourceValueAPI[e.Account]()(ctx)
			if ctx.IsAborted() {
				return
			}
			response[e.Account](ctx).CreatedOrUpdated(srv.Accounts().CreateOrUpdate(ctx.Request.Context(), RequireResource[e.Account](ctx)))
		}
	}
}
func registerSystemRoutes(v1 *gin.RouterGroup, srv *server.Service) {
	v1.POST("/accounts/:id/impact-previews", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.CreateImpactPreview(ctx.Request.Context(), e.AccountID(ctx.Param("id")), RequireResource[e.SystemAction](ctx))
		response[e.ImpactPreview](ctx).Created(result, err)
	})
}
