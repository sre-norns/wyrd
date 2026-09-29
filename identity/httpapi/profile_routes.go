package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

func personalProfileRead(srv *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		profile, found, err := srv.PersonalProfile().Get(ctx.Request.Context())
		if err == nil && found {
			ctx.Header("ETag", server.ETag(profile.Revision))
		}
		response[e.PersonalProfile](ctx).Found(profile, found, err)
	}
}

func personalProfileAccounts(srv *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		accounts, page, err := srv.PersonalProfile().AccessibleAccounts(ctx.Request.Context(), requireSearchQuery(ctx))
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, listPage(accounts, page))
	}
}

func personalProfileUpdate(srv *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1048577))
		if err != nil || len(body) > 1048576 {
			writeProblem(ctx, &server.Problem{Status: http.StatusRequestEntityTooLarge, Code: "body-too-large", Detail: "The request exceeds 1 MiB."})
			return
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(body, &fields); err != nil || fields == nil {
			writeProblem(ctx, &server.Problem{Status: http.StatusBadRequest, Code: "invalid-json", Detail: "Invalid JSON body."})
			return
		}
		raw, present := fields["display_name"]
		if !present || len(fields) != 1 {
			writeProblem(ctx, &server.Problem{
				Status: http.StatusUnprocessableEntity,
				Code:   "validation",
				Detail: "Only display_name can be changed.",
				Fields: map[string]string{"display_name": "Provide display_name as a string or null."},
			})
			return
		}
		var displayName *string
		if string(raw) != "null" {
			var value string
			if err = json.Unmarshal(raw, &value); err != nil {
				writeProblem(ctx, &server.Problem{
					Status: http.StatusUnprocessableEntity,
					Code:   "validation",
					Detail: "The display name must be a string or null.",
					Fields: map[string]string{"display_name": "Provide display_name as a string or null."},
				})
				return
			}
			displayName = &value
		}
		profile, err := srv.PersonalProfile().Update(ctx.Request.Context(), e.PersonalProfile{DisplayName: displayName})
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		ctx.Header("ETag", server.ETag(profile.Revision))
		ctx.JSON(http.StatusOK, profile)
	}
}

func signInMethodList(srv *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		methods, err := srv.SignInMethods().List(ctx.Request.Context())
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		// A user has a sign-in method per provider at most: one page holds them all.
		total := int64(len(methods))
		ctx.JSON(http.StatusOK, listPage(methods, manifest.Page{Limit: dbstore.DefaultPageLimit, Total: &total}))
	}
}

func signInMethodRead(srv *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		method, found, err := srv.SignInMethods().Get(ctx.Request.Context(), ctx.Param("id"))
		if err == nil && found {
			ctx.Header("ETag", server.ETag(method.Revision))
		}
		response[e.SignInMethod](ctx).Found(method, found, err)
	}
}

// signInMethodUpdate accepts only {"status":"revoked"}, the explicit removal
// of a provider method, with the current revision in If-Match.
func signInMethodUpdate(srv *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1048577))
		if err != nil || len(body) > 1048576 {
			writeProblem(ctx, &server.Problem{Status: http.StatusRequestEntityTooLarge, Code: "body-too-large", Detail: "The request exceeds 1 MiB."})
			return
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(body, &fields); err != nil || fields == nil {
			writeProblem(ctx, &server.Problem{Status: http.StatusBadRequest, Code: "invalid-json", Detail: "Invalid JSON body."})
			return
		}
		var status string
		raw, present := fields["status"]
		if !present || len(fields) != 1 || json.Unmarshal(raw, &status) != nil {
			writeProblem(ctx, &server.Problem{Status: http.StatusUnprocessableEntity, Code: "validation", Detail: "Only status can be changed.", Fields: map[string]string{"status": "Provide status as revoked."}})
			return
		}
		method, err := srv.SignInMethods().Revoke(ctx.Request.Context(), e.SignInMethod{ID: ctx.Param("id"), Status: status})
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		ctx.Header("ETag", server.ETag(method.Revision))
		ctx.JSON(http.StatusOK, method)
	}
}
