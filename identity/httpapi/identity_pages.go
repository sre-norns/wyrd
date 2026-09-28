package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
)

func identityAccess(s *server.Service, form string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		page := oauthPage{AccessForm: form, Retry: "/sign-in", Title: "Create account"}
		kind := "register"
		if form == "forgot-password" || form == "reset-password" {
			kind = "reset"
			page.Title = "Reset password"
		}
		if form == "verify-email" {
			page.Title = "Finish account setup"
		}
		if err := ctx.Request.ParseForm(); err != nil {
			renderIdentityPage(ctx, 400, "complete", oauthPage{Title: "Invalid request", Message: "The service cannot read this form. Try again.", Failed: true, Retry: "/sign-in"})
			return
		}
		page.Token = ctx.Request.Form.Get("token")
		if form == "register" {
			// Provider registration can resume the web login that linked here.
			page.Providers = s.SignInProviders()
			if ctx.Request.Form.Get("client_id") == s.WebClientID() {
				page.ClientID, page.RedirectURI, page.Challenge, page.State = s.WebClientID(), ctx.Request.Form.Get("redirect_uri"), ctx.Request.Form.Get("code_challenge"), ctx.Request.Form.Get("state")
			}
		}
		if ctx.Request.Method == http.MethodPost {
			page.Email = ctx.Request.PostForm.Get("email")
			page.AccountName = ctx.Request.PostForm.Get("account_name")
		}
		var err error
		if kind == "register" && !s.RegistrationEnabled() {
			renderIdentityPage(ctx, 403, "complete", oauthPage{Title: "Account creation requires an administrator", Message: "Contact your service administrator to create an account.", Failed: true, Retry: "/sign-in"})
			return
		}
		completion := form == "verify-email" || form == "reset-password"
		if completion {
			var detail server.IdentityLinkDetails
			detail, err = s.IdentityLink(ctx.Request.Context(), kind, page.Token)
			if err == nil {
				page.Email, page.AccountName = detail.Email, detail.AccountName
				if ctx.Request.Method == http.MethodPost {
					password := ctx.Request.PostForm.Get("password")
					if password != ctx.Request.PostForm.Get("confirm_password") {
						err = &server.Problem{Status: 422, Detail: "The passwords do not match."}
					} else {
						err = s.CompleteIdentityLink(ctx.Request.Context(), kind, page.Token, password)
					}
					if err == nil {
						title, message := "Account created", "Your email is verified. Log in to open your account."
						if kind == "reset" {
							title, message = "Password updated", "Your previous user sessions are revoked. Log in with your new password."
						}
						renderIdentityPage(ctx, 200, "access-complete", oauthPage{Title: title, Message: message})
						return
					}
				}
			}
		} else if ctx.Request.Method == http.MethodPost {
			err = s.RequestIdentityLink(ctx.Request.Context(), kind, page.Email, page.AccountName)
			if err == nil {
				renderIdentityPage(ctx, 200, "access-complete", oauthPage{Title: "Check your email", Message: "If this address can complete the request, an email contains the next step. Allow a minute before requesting another link. Links expire after 30 minutes."})
				return
			}
		} else if !s.IdentityMailAvailable() {
			err = &server.Problem{Status: 503, Detail: "Account email is not configured. Contact your service administrator."}
		}
		status := 200
		if err != nil {
			status, page.Failed, page.Message = 500, true, "The service cannot complete this request. Try again later."
			var p *server.Problem
			if errors.As(err, &p) {
				status, page.Message = p.Status, p.Detail
				if p.Code == "invalid-link" {
					retry := "/oauth/register"
					if kind == "reset" {
						retry = "/oauth/forgot-password"
					}
					renderIdentityPage(ctx, status, "complete", oauthPage{Title: "Link unavailable", Message: p.Detail, Failed: true, Retry: retry, RetryLabel: "Request a new link"})
					return
				}
			}
		}
		renderIdentityPage(ctx, status, "account-access", page)
	}
}

func renderIdentityPage(ctx *gin.Context, status int, name string, page oauthPage) {
	page.WebLogin = true
	if page.RetryLabel == "" {
		page.RetryLabel = "Back to login"
	}
	renderOAuthPage(ctx, status, name, page)
}
