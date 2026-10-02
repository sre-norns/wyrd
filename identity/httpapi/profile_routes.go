package httpapi

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
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
		var input e.PersonalProfile
		fields, err := resource.DecodeInput(body, &input, true)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		if _, ok := fields["display_name"]; !ok {
			writeProblem(ctx, &resource.InputError{Field: "spec.displayName", Detail: "Provide a string or null."})
			return
		}
		profile, err := srv.PersonalProfile().Update(ctx.Request.Context(), input)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		ctx.Header("ETag", server.ETag(profile.Revision))
		response[e.PersonalProfile](ctx).send(http.StatusOK, profile)
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
		response[e.SignInMethod](ctx).List(methods, manifest.Page{Limit: dbstore.DefaultPageLimit, Total: &total}, nil)
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

// signInMethodUpdate accepts only {"operation":"revoke"}, the explicit removal
// of a provider method, with the current revision in If-Match.
func signInMethodUpdate(srv *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1048577))
		if err != nil || len(body) > 1048576 {
			writeProblem(ctx, &server.Problem{Status: http.StatusRequestEntityTooLarge, Code: "body-too-large", Detail: "The request exceeds 1 MiB."})
			return
		}
		var input e.SignInMethod
		fields, err := resource.DecodeInput(body, &input, true)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		if len(fields) != 1 || input.Status != "revoked" {
			writeProblem(ctx, &resource.InputError{Field: "operation", Detail: "Provide revoke."})
			return
		}
		input.ID = ctx.Param("id")
		method, err := srv.SignInMethods().Revoke(ctx.Request.Context(), input)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		ctx.Header("ETag", server.ETag(method.Revision))
		response[e.SignInMethod](ctx).send(http.StatusOK, method)
	}
}
