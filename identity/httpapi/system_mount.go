package httpapi

import (
	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/bark"
)

// MountSystem serves shared system identity operations with authentication,
// canonical resources, ETags and transaction-bound replay. Opt in after Mount;
// a host must remove its equivalent routes before adopting it. Host-specific
// overview, health, capacity, policy and streaming routes remain with the host.
func MountSystem(router *gin.Engine, srv *server.Service) {
	system := router.Group("/v1/system", bark.ContentTypeAPI(), resourceContentType(), authenticated(srv))
	system.POST("/account-invitations/:id/deliveries", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		out, err := srv.RequestInvitationDelivery(ctx.Request.Context(), ctx.Param("id"), RequireResource[e.SystemAction](ctx))
		response[e.SystemInvitation](ctx).Created(server.SystemInvitationProjection(out), err)
	})
	system.POST("/account-invitations/:id/revocations", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		out, err := srv.RevokeInvitation(ctx.Request.Context(), ctx.Param("id"), RequireResource[e.SystemAction](ctx))
		response[e.SystemInvitation](ctx).Created(server.SystemInvitationProjection(out), err)
	})
	system.POST("/accounts/:id/owner-invitations", ResourceValueAPI[e.FirstOwnerInvitation](), func(ctx *gin.Context) {
		out, err := srv.CreateFirstOwnerInvitation(ctx.Request.Context(), e.AccountID(ctx.Param("id")), RequireResource[e.FirstOwnerInvitation](ctx))
		response[e.SystemInvitationCreated](ctx).Created(e.SystemInvitationCreated{SystemInvitation: server.SystemInvitationProjection(out), Token: out.Token}, err)
	})
	system.POST("/accounts", ResourceValueAPI[e.SystemAccountCreate](), func(ctx *gin.Context) {
		result, err := srv.CreateSystemAccount(ctx.Request.Context(), RequireResource[e.SystemAccountCreate](ctx))
		response[e.SystemAccountCreated](ctx).Created(result, err)
	})
	system.GET("/accounts", func(ctx *gin.Context) {
		q, err := systemQuery(ctx)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.SystemAccounts(ctx.Request.Context(), q)
		response[e.SystemPage[e.SystemAccount]](ctx).Found(result, true, err)
	})
	system.GET("/accounts/:id", func(ctx *gin.Context) {
		result, err := srv.SystemAccount(ctx.Request.Context(), e.AccountID(ctx.Param("id")))
		response[e.SystemAccount](ctx).Found(result, true, err)
	})
	system.GET("/accounts/:id/memberships", func(ctx *gin.Context) {
		q, err := systemQuery(ctx)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.SystemMemberships(ctx.Request.Context(), e.AccountID(ctx.Param("id")), q)
		response[e.SystemPage[e.SystemMembership]](ctx).Found(result, true, err)
	})
	system.GET("/accounts/:id/invitations", func(ctx *gin.Context) {
		q, err := systemQuery(ctx)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.SystemInvitations(ctx.Request.Context(), e.AccountID(ctx.Param("id")), q)
		response[e.SystemPage[e.SystemInvitation]](ctx).Found(result, true, err)
	})
	system.GET("/accounts/:id/impact-previews", func(ctx *gin.Context) {
		q, err := systemQuery(ctx)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.ImpactPreviews(ctx.Request.Context(), e.AccountID(ctx.Param("id")), q)
		response[e.SystemPage[e.ImpactPreview]](ctx).Found(result, true, err)
	})
	system.GET("/accounts/:id/owner-recoveries", func(ctx *gin.Context) {
		q, err := systemQuery(ctx)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.OwnerRecoveries(ctx.Request.Context(), e.AccountID(ctx.Param("id")), q)
		response[e.SystemPage[e.OwnerRecovery]](ctx).Found(result, true, err)
	})
	system.GET("/owner-recoveries/:id", func(ctx *gin.Context) {
		result, err := srv.OwnerRecovery(ctx.Request.Context(), ctx.Param("id"))
		response[e.OwnerRecovery](ctx).Found(result, true, err)
	})
	system.GET("/deletion-requests", func(ctx *gin.Context) {
		q, err := systemQuery(ctx)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.DeletionRequests(ctx.Request.Context(), q)
		response[e.SystemPage[e.AccountDeletionRequest]](ctx).Found(result, true, err)
	})
	system.GET("/deletion-requests/:id", func(ctx *gin.Context) {
		result, err := srv.DeletionRequest(ctx.Request.Context(), ctx.Param("id"))
		response[e.AccountDeletionRequest](ctx).Found(result, true, err)
	})
	system.GET("/activity", func(ctx *gin.Context) {
		q, err := systemQuery(ctx)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		result, err := srv.SystemActivity(ctx.Request.Context(), q)
		response[e.SystemPage[e.SystemActivity]](ctx).Found(result, true, err)
	})
	system.POST("/accounts/:id/impact-previews", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.CreateImpactPreview(ctx.Request.Context(), e.AccountID(ctx.Param("id")), RequireResource[e.SystemAction](ctx))
		response[e.ImpactPreview](ctx).Created(result, err)
	})
	system.PATCH("/accounts/:id", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.ChangeAccountLifecycle(ctx.Request.Context(), e.AccountID(ctx.Param("id")), RequireResource[e.SystemAction](ctx))
		response[e.SystemAccount](ctx).Found(result, true, err)
	})
	system.PATCH("/account-memberships/:id", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.RevokeSystemMembership(ctx.Request.Context(), ctx.Param("id"), RequireResource[e.SystemAction](ctx))
		response[e.SystemMembership](ctx).Found(result, true, err)
	})
	system.POST("/accounts/:id/owner-recoveries", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.CreateOwnerRecovery(ctx.Request.Context(), e.AccountID(ctx.Param("id")), RequireResource[e.SystemAction](ctx))
		response[e.OwnerRecovery](ctx).Created(result, err)
	})
	system.PATCH("/owner-recoveries/:id", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.CompleteOwnerRecovery(ctx.Request.Context(), ctx.Param("id"), RequireResource[e.SystemAction](ctx))
		response[e.OwnerRecovery](ctx).Found(result, true, err)
	})
	system.POST("/accounts/:id/step-up-authorizations", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.CreateStepUp(ctx.Request.Context(), e.AccountID(ctx.Param("id")), RequireResource[e.SystemAction](ctx))
		response[e.StepUpAuthorization](ctx).Created(result, err)
	})
	system.POST("/accounts/:id/deletion-requests", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.RequestAccountDeletion(ctx.Request.Context(), e.AccountID(ctx.Param("id")), RequireResource[e.SystemAction](ctx))
		response[e.AccountDeletionRequest](ctx).Created(result, err)
	})
	system.POST("/deletion-requests/:id/approvals", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.ApproveAccountDeletion(ctx.Request.Context(), ctx.Param("id"), RequireResource[e.SystemAction](ctx))
		response[e.AccountDeletionRequest](ctx).Created(result, err)
	})
	system.PATCH("/deletion-requests/:id", ResourceValueAPI[e.SystemAction](), func(ctx *gin.Context) {
		result, err := srv.ChangeDeletionRequest(ctx.Request.Context(), ctx.Param("id"), RequireResource[e.SystemAction](ctx))
		response[e.AccountDeletionRequest](ctx).Found(result, true, err)
	})
	for _, kind := range []string{"audit-events", "changes"} {
		system.GET("/"+kind, func(ctx *gin.Context) {
			q, err := systemQuery(ctx)
			if err != nil {
				writeProblem(ctx, err)
				return
			}
			q.Kind = "audit"
			if ctx.FullPath() == "/v1/system/changes" {
				q.Kind = "change"
			}
			result, err := srv.SystemActivity(ctx.Request.Context(), q)
			response[e.SystemPage[e.SystemActivity]](ctx).Found(result, true, err)
		})
	}

	system.GET("/configuration", func(ctx *gin.Context) {
		result, err := srv.SystemConfiguration(ctx.Request.Context())
		response[e.SystemConfiguration](ctx).Found(result, true, err)
	})

}
