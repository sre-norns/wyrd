package httpapi

import (
	server "github.com/sre-norns/wyrd/identity"
	expbench "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/pages"
	"github.com/sre-norns/wyrd/pkg/bark"

	"github.com/gin-gonic/gin"
)

// Config contains the presentation settings for this mounted identity service:
// the pages' brand, theme and product wording.
type Config = pages.Config

// Mount serves the shared OAuth and identity API in a product's router.
func Mount(router *gin.Engine, srv *server.Service, cfg Config) {
	router.Use(func(ctx *gin.Context) { ctx.Set("identity.presentation", cfg); ctx.Next() })
	router.Use(authenticationRate(srv))
	oauthReply := oauthResponse
	router.GET("/.well-known/oauth-authorization-server", bark.ContentTypeAPI(), func(ctx *gin.Context) {
		result, err := srv.OAuth().Metadata(ctx.Request.Context())
		oauthReply(ctx, result, err)
	})
	router.GET("/oauth/authorize", browserApproval(srv))
	router.GET("/oauth/invitations/start", invitationStart(srv))
	router.GET("/oauth/invitations/authorize", invitationAuthorize(srv))
	router.POST("/oauth/invitations/authorize", invitationAuthorize(srv))
	router.GET("/oauth/invitations/providers/:provider/start", invitationProviderStart(srv))
	router.GET("/oauth/providers/:provider/start", providerStart(srv))
	router.GET("/oauth/providers/:provider/callback", providerCallback(srv))
	router.POST("/oauth/providers/registration", providerRegistration(srv))
	router.GET("/oauth/providers/confirmation", providerConfirmation(srv))
	router.POST("/oauth/providers/confirmation", providerConfirmation(srv))
	router.GET("/oauth/login", browserLogin(srv))
	router.POST("/oauth/login", browserLogin(srv))
	router.POST("/oauth/device_authorization", bark.ContentTypeAPI(), func(ctx *gin.Context) {
		if err := ctx.Request.ParseForm(); err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.OAuth().DeviceAuthorization(ctx.Request.Context(), ctx.Request.PostForm)
		oauthReply(ctx, result, err)
	})
	router.POST("/oauth/token", bark.ContentTypeAPI(), func(ctx *gin.Context) {
		if err := ctx.Request.ParseForm(); err != nil {
			writeProblem(ctx, err)
			return
		}
		requestContext := srv.RequestContext(ctx.Request.Context(), server.Request{
			Origin:    ctx.GetHeader("Origin"),
			IPAddress: ctx.ClientIP(),
			UserAgent: ctx.GetHeader("User-Agent"),
		})
		result, err := srv.OAuth().Token(requestContext, ctx.Request.PostForm)
		oauthReply(ctx, result, err)
	})
	router.POST("/oauth/revoke", bark.ContentTypeAPI(), func(ctx *gin.Context) {
		if err := ctx.Request.ParseForm(); err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.OAuth().Revoke(ctx.Request.Context(), ctx.Request.PostForm)
		oauthReply(ctx, result, err)
	})
	router.POST("/oauth/authorize", browserApproval(srv))
	router.GET("/oauth/device", browserApproval(srv))
	router.POST("/oauth/device", browserApproval(srv))
	for _, form := range []string{"register", "forgot-password", "verify-email", "reset-password"} {
		router.GET("/oauth/"+form, identityAccess(srv, form))
		router.POST("/oauth/"+form, identityAccess(srv, form))
	}
	v1 := router.Group("/v1", bark.ContentTypeAPI(), resourceContentType())
	v1.GET("/service-configuration", func(ctx *gin.Context) {
		response[expbench.ServiceConfiguration](ctx).Found(srv.ServiceConfig().Get(ctx.Request.Context()))
	})
	v1.Use(authenticated(srv))
	v1.GET("/principal", func(ctx *gin.Context) {
		response[expbench.Principal](ctx).Found(srv.Principal().Get(ctx.Request.Context()))
	})
	v1.GET("/profile", personalProfileRead(srv))
	v1.GET("/profile/accounts", searchable(), personalProfileAccounts(srv))
	v1.PATCH("/profile", personalProfileUpdate(srv))
	v1.GET("/profile/sign-in-methods", signInMethodList(srv))
	v1.GET("/profile/sign-in-methods/:id", signInMethodRead(srv))
	v1.PATCH("/profile/sign-in-methods/:id", signInMethodUpdate(srv))
	v1.GET("/sessions", searchable(), func(ctx *gin.Context) {
		response[expbench.Session](ctx).List(
			srv.Sessions().List(ctx.Request.Context(), requireSearchQuery(ctx)),
		)
	})
	v1.GET("/sessions/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.Session](ctx).Found(
			srv.Sessions().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.SessionID](ctx)),
		)
	})
	v1.PATCH("/sessions/:id", bark.ResourceAPI(), ResourceValueAPI[expbench.Session](), func(ctx *gin.Context) {
		response[expbench.Session](ctx).CreatedOrUpdated(
			srv.Sessions().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.Session](ctx)),
		)
	})
	v1.GET("/accounts", searchable(), func(ctx *gin.Context) {
		response[expbench.Account](ctx).List(
			srv.Accounts().List(ctx.Request.Context(), requireSearchQuery(ctx)),
		)
	})
	v1.POST("/accounts", IdempotencyAPI(), ResourceValueAPI[expbench.Account](), func(ctx *gin.Context) {
		response[expbench.Account](ctx).CreatedOrUpdated(
			srv.Accounts().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.Account](ctx)),
		)
	})
	v1.GET("/accounts/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.Account](ctx).Found(
			srv.Accounts().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.AccountID](ctx)),
		)
	})
	v1.PATCH("/accounts/:id", accountUpdate(srv))
	v1.GET("/accounts/:id/memberships", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.AccountMembership](ctx).List(
			srv.AccountMemberships().List(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AccountID](ctx), requireSearchQuery(ctx)),
		)
	})
	v1.POST("/accounts/:id/memberships", IdempotencyAPI(), bark.ResourceAPI(), ResourceValueAPI[expbench.AccountMembership](), func(ctx *gin.Context) {
		response[expbench.AccountMembership](ctx).Created(
			srv.AccountMemberships().Create(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AccountID](ctx), RequireResource[expbench.AccountMembership](ctx)),
		)
	})
	v1.GET("/account-memberships/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.AccountMembership](ctx).Found(
			srv.AccountMemberships().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.AccountMembershipID](ctx)),
		)
	})
	v1.PATCH("/account-memberships/:id", bark.ResourceAPI(), ResourceValueAPI[expbench.AccountMembership](), func(ctx *gin.Context) {
		response[expbench.AccountMembership](ctx).CreatedOrUpdated(
			srv.AccountMemberships().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.AccountMembership](ctx)),
		)
	})
	v1.GET("/accounts/:id/invitations", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.AccountInvitation](ctx).List(
			srv.AccountInvitations().List(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AccountID](ctx), requireSearchQuery(ctx)),
		)
	})
	v1.POST("/accounts/:id/invitations", IdempotencyAPI(), bark.ResourceAPI(), ResourceValueAPI[expbench.AccountInvitation](), func(ctx *gin.Context) {
		response[expbench.AccountInvitation](ctx).Created(
			srv.AccountInvitations().Create(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AccountID](ctx), RequireResource[expbench.AccountInvitation](ctx)),
		)
	})
	v1.GET("/account-invitations/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.AccountInvitation](ctx).Found(
			srv.AccountInvitations().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.AccountInvitationID](ctx)),
		)
	})
	v1.PATCH("/account-invitations/:id", bark.ResourceAPI(), ResourceValueAPI[expbench.AccountInvitation](), func(ctx *gin.Context) {
		response[expbench.AccountInvitation](ctx).CreatedOrUpdated(
			srv.AccountInvitations().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.AccountInvitation](ctx)),
		)
	})
	v1.POST("/account-invitations/:id/acceptance", IdempotencyAPI(), bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.AccountMembership](ctx).Found(
			srv.AccountInvitations().Accept(ctx.Request.Context(), RequireResourceTypeID[expbench.AccountInvitationID](ctx)),
		)
	})
	// Resend: a new link by email; earlier links stop working. The service
	// checks the caller administers the invitation's account.
	v1.POST("/account-invitations/:id/deliveries", IdempotencyAPI(), ResourceValueAPI[expbench.SystemAction](), func(ctx *gin.Context) {
		out, err := srv.RequestInvitationDelivery(ctx.Request.Context(), ctx.Param("id"), RequireResource[expbench.SystemAction](ctx))
		response[expbench.AccountInvitation](ctx).Created(out, err)
	})
	v1.GET("/accounts/:id/agent-identities", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.AgentIdentity](ctx).List(
			srv.AgentIdentities().List(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AccountID](ctx), requireSearchQuery(ctx)),
		)
	})
	v1.POST("/accounts/:id/agent-identities", IdempotencyAPI(), bark.ResourceAPI(), ResourceValueAPI[expbench.AgentIdentity](), func(ctx *gin.Context) {
		response[expbench.AgentIdentity](ctx).Created(
			srv.AgentIdentities().Create(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AccountID](ctx), RequireResource[expbench.AgentIdentity](ctx)),
		)
	})
	v1.GET("/agent-identities/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.AgentIdentity](ctx).Found(
			srv.AgentIdentities().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.AgentIdentityID](ctx)),
		)
	})
	v1.PATCH("/agent-identities/:id", bark.ResourceAPI(), ResourceValueAPI[expbench.AgentIdentity](), func(ctx *gin.Context) {
		response[expbench.AgentIdentity](ctx).CreatedOrUpdated(
			srv.AgentIdentities().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.AgentIdentity](ctx)),
		)
	})
	v1.GET("/agent-identities/:id/tokens", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.AgentIdentityToken](ctx).List(
			srv.AgentIdentityTokens().List(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AgentIdentityID](ctx), requireSearchQuery(ctx)),
		)
	})
	v1.POST("/agent-identities/:id/tokens", IdempotencyAPI(), bark.ResourceAPI(), ResourceValueAPI[expbench.AgentIdentityToken](), func(ctx *gin.Context) {
		response[expbench.AgentIdentityToken](ctx).Created(
			srv.AgentIdentityTokens().Create(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AgentIdentityID](ctx), RequireResource[expbench.AgentIdentityToken](ctx)),
		)
	})
	v1.GET("/agent-identity-tokens/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.AgentIdentityToken](ctx).Found(
			srv.AgentIdentityTokens().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.AgentIdentityTokenID](ctx)),
		)
	})
	v1.PATCH("/agent-identity-tokens/:id", bark.ResourceAPI(), ResourceValueAPI[expbench.AgentIdentityToken](), func(ctx *gin.Context) {
		response[expbench.AgentIdentityToken](ctx).CreatedOrUpdated(
			srv.AgentIdentityTokens().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.AgentIdentityToken](ctx)),
		)
	})
	v1.GET("/projects", searchable(), func(ctx *gin.Context) {
		reqCtx := server.WithProjectSearch(ctx.Request.Context(), ctx.Query("q"))
		response[expbench.Project](ctx).List(
			srv.Projects().List(reqCtx, requireSearchQuery(ctx)),
		)
	})
	v1.GET("/accounts/:id/projects", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		reqCtx := server.WithProjectSearch(ctx.Request.Context(), ctx.Query("q"))
		response[expbench.Project](ctx).List(
			srv.Projects().ListForAccount(reqCtx,
				RequireResourceTypeID[expbench.AccountID](ctx), requireSearchQuery(ctx)),
		)
	})
	v1.POST("/accounts/:id/projects", IdempotencyAPI(), bark.ResourceAPI(), ResourceValueAPI[expbench.Project](), func(ctx *gin.Context) {
		response[expbench.Project](ctx).Created(
			srv.Projects().Create(ctx.Request.Context(), RequireResource[expbench.Project](ctx)),
		)
	})
	v1.GET("/projects/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.Project](ctx).Found(
			srv.Projects().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.ProjectID](ctx)),
		)
	})
	v1.PATCH("/projects/:id", bark.ResourceAPI(), ResourceValueAPI[expbench.Project](), func(ctx *gin.Context) {
		response[expbench.Project](ctx).CreatedOrUpdated(
			srv.Projects().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.Project](ctx)),
		)
	})
	v1.GET("/projects/:id/memberships", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.ProjectMembership](ctx).List(
			srv.ProjectMemberships().List(ctx.Request.Context(),
				RequireResourceTypeID[expbench.ProjectID](ctx), requireSearchQuery(ctx)),
		)
	})
	v1.POST("/projects/:id/memberships", IdempotencyAPI(), bark.ResourceAPI(), ResourceValueAPI[expbench.ProjectMembership](), func(ctx *gin.Context) {
		response[expbench.ProjectMembership](ctx).Created(
			srv.ProjectMemberships().Create(ctx.Request.Context(),
				RequireResourceTypeID[expbench.ProjectID](ctx), RequireResource[expbench.ProjectMembership](ctx)),
		)
	})
	v1.GET("/project-memberships/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.ProjectMembership](ctx).Found(
			srv.ProjectMemberships().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.ProjectMembershipID](ctx)),
		)
	})
	v1.PATCH("/project-memberships/:id", bark.ResourceAPI(), ResourceValueAPI[expbench.ProjectMembership](), func(ctx *gin.Context) {
		response[expbench.ProjectMembership](ctx).CreatedOrUpdated(
			srv.ProjectMemberships().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.ProjectMembership](ctx)),
		)
	})
	v1.GET("/projects/:id/agent-authorizations", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.AgentAuthorization](ctx).List(
			srv.AgentAuthorizations().List(ctx.Request.Context(),
				RequireResourceTypeID[expbench.ProjectID](ctx), requireSearchQuery(ctx)),
		)
	})
	v1.POST("/projects/:id/agent-authorizations", IdempotencyAPI(), bark.ResourceAPI(), ResourceValueAPI[expbench.AgentAuthorization](), func(ctx *gin.Context) {
		response[expbench.AgentAuthorization](ctx).Created(
			srv.AgentAuthorizations().Create(ctx.Request.Context(),
				RequireResourceTypeID[expbench.ProjectID](ctx), RequireResource[expbench.AgentAuthorization](ctx)),
		)
	})
	v1.GET("/agent-authorizations/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
		response[expbench.AgentAuthorization](ctx).Found(
			srv.AgentAuthorizations().Get(ctx.Request.Context(), RequireResourceTypeID[expbench.AgentAuthorizationID](ctx)),
		)
	})
	v1.PATCH("/agent-authorizations/:id", bark.ResourceAPI(), ResourceValueAPI[expbench.AgentAuthorization](), func(ctx *gin.Context) {
		response[expbench.AgentAuthorization](ctx).CreatedOrUpdated(
			srv.AgentAuthorizations().CreateOrUpdate(ctx.Request.Context(), RequireResource[expbench.AgentAuthorization](ctx)),
		)
	})
	// Project access management: whom a project may add, and where a machine
	// identity is granted. Read models over identity records only.
	v1.GET("/projects/:id/member-candidates", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.ProjectMemberCandidate](ctx).List(
			srv.Directory().ProjectMemberCandidates(ctx.Request.Context(),
				RequireResourceTypeID[expbench.ProjectID](ctx), ctx.Query("q"), requireSearchQuery(ctx)),
		)
	})
	v1.GET("/projects/:id/agent-candidates", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.ProjectAgentCandidate](ctx).List(
			srv.Directory().ProjectAgentCandidates(ctx.Request.Context(),
				RequireResourceTypeID[expbench.ProjectID](ctx), ctx.Query("q"), requireSearchQuery(ctx)),
		)
	})
	v1.GET("/agent-identities/:id/project-authorizations", bark.ResourceAPI(), searchable(), func(ctx *gin.Context) {
		response[expbench.AgentProjectAuthorization](ctx).List(
			srv.Directory().AgentProjectAuthorizations(ctx.Request.Context(),
				RequireResourceTypeID[expbench.AgentIdentityID](ctx), requireSearchQuery(ctx)),
		)
	})
	registerSystemRoutes(v1, srv)
}

// Authenticate resolves bearer credentials and installs the principal and request context.
func Authenticate(service *server.Service) gin.HandlerFunc { return authenticated(service) }
