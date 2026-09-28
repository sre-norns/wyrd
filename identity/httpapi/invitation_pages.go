package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
)

const invitationCookie = "exp-bench-invitation"

// legacyInvitationCookie is the cookie name before the Exp-Bench rename. The
// service reads it so that invitations started before an upgrade continue.
// Deprecated: Use invitationCookie instead
const legacyInvitationCookie = "experibench-invitation"

func invitationState(ctx *gin.Context) string {
	if cookie, err := ctx.Cookie(invitationCookie); err == nil {
		return cookie
	}
	cookie, _ := ctx.Cookie(legacyInvitationCookie)
	return cookie
}

func setInvitationCookie(ctx *gin.Context, value string, maxAge int) {
	// Local HTTP development is the only permitted non-secure cookie context.
	http.SetCookie(ctx.Writer, &http.Cookie{Name: invitationCookie, Value: value, Path: "/oauth/invitations", MaxAge: maxAge, HttpOnly: true, Secure: !localHost(ctx) || ctx.Request.TLS != nil, SameSite: http.SameSiteLaxMode})
}

func invitationStart(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("Referrer-Policy", "no-referrer")
		cookie, err := s.StartInvitation(ctx.Request.Context(), ctx.Query("invitation_id"), ctx.Query("token"))
		if err != nil {
			invitationFailure(ctx, err)
			return
		}
		setInvitationCookie(ctx, cookie, 600)
		ctx.Redirect(http.StatusSeeOther, "/invitations/continue")
	}
}

func invitationAuthorize(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := ctx.Request.ParseForm(); err != nil {
			invitationFailure(ctx, &server.Problem{Status: 400, Detail: "The service cannot read this form."})
			return
		}
		cookie := invitationState(ctx)
		detail, err := s.ReviewInvitation(ctx.Request.Context(), cookie, ctx.Request.Form)
		if err != nil {
			invitationFailure(ctx, err)
			return
		}
		page := oauthPage{Title: "Join " + detail.AccountName, WebLogin: true, Invitation: &detail}
		page.Providers = detail.Providers
		status := 200
		if ctx.Request.Method == http.MethodPost {
			var target server.AuthorizationRedirect
			target, err = s.JoinInvitation(ctx.Request.Context(), cookie, ctx.Request.PostForm)
			if err == nil {
				ctx.Header("Cache-Control", "no-store")
				ctx.Header("Referrer-Policy", "no-referrer")
				ctx.Redirect(http.StatusSeeOther, target.URL)
				return
			}
			status, page.Failed, page.Message = 500, true, "The service cannot complete this invitation. Try again."
			var p *server.Problem
			if errors.As(err, &p) {
				status, page.Message = p.Status, p.Detail
			}
		}
		renderOAuthPage(ctx, status, "invitation", page)
	}
}

func invitationFailure(ctx *gin.Context, err error) {
	status, message := 500, "The service cannot open this invitation. Try again later."
	var p *server.Problem
	if errors.As(err, &p) {
		status = p.Status
		message = "This invitation is unavailable. Use the latest invitation email or contact the inviter."
		if p.Code == "account-inactive" || p.Code == "recipient-unavailable" || p.Code == "membership-unavailable" {
			message = p.Detail
		}
	}
	renderIdentityPage(ctx, status, "complete", oauthPage{Title: "Invitation unavailable", Message: message, Failed: true, Retry: "/sign-in"})
}

const InvitationCookie = invitationCookie

const LegacyInvitationCookie = legacyInvitationCookie
