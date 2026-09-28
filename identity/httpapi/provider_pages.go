package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
)

// providerCookie holds the opaque provider transaction secret between the
// start request and the provider callback.
const providerCookie = "exp-bench-provider"

func providerLabel(name string) string {
	if name == "oidc" {
		return "OpenID Connect"
	}
	if name == "github" {
		return "GitHub"
	}
	return "Google"
}

func localHost(ctx *gin.Context) bool {
	host := ctx.Request.Host
	return strings.HasPrefix(host, "localhost:") || strings.HasPrefix(host, "127.0.0.1:") || host == "localhost" || host == "127.0.0.1"
}

func setProviderCookie(ctx *gin.Context, value string, maxAge int) {
	// Lax lets the browser send the cookie on the top-level provider redirect.
	http.SetCookie(ctx.Writer, &http.Cookie{Name: providerCookie, Value: value, Path: "/oauth/providers", MaxAge: maxAge, HttpOnly: true, Secure: !localHost(ctx) || ctx.Request.TLS != nil, SameSite: http.SameSiteLaxMode})
}

// providerRate applies the provider request limit and reports a rejection.
func providerRate(ctx *gin.Context, s *server.Service, purpose string) bool {
	if err := s.AllowProviderAuthentication(ctx.Request.Context(), ctx.ClientIP(), ctx.Param("provider")); err != nil {
		providerFailure(ctx, ctx.Param("provider"), server.ProviderResult{Purpose: purpose}, err)
		return false
	}
	return true
}

func providerStart(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !providerRate(ctx, s, ctx.Query("purpose")) {
			return
		}
		if err := ctx.Request.ParseForm(); err != nil {
			renderIdentityPage(ctx, 400, "complete", oauthPage{Title: "Invalid request", Message: "The service cannot read this request. Try again.", Failed: true, Retry: "/sign-in"})
			return
		}
		form := ctx.Request.Form
		purpose := form.Get("purpose")
		if purpose == "invitation" {
			renderIdentityPage(ctx, 400, "complete", oauthPage{Title: "Invalid request", Message: "Start sign-in from the invitation page.", Failed: true, Retry: "/sign-in"})
			return
		}
		cookie, location, err := s.StartProvider(ctx.Request.Context(), ctx.Param("provider"), purpose, form)
		if err != nil {
			providerFailure(ctx, ctx.Param("provider"), server.ProviderResult{Purpose: purpose, Form: form}, err)
			return
		}
		setProviderCookie(ctx, cookie, 600)
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("Referrer-Policy", "no-referrer")
		ctx.Redirect(http.StatusSeeOther, location)
	}
}

func invitationProviderStart(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !providerRate(ctx, s, "invitation") {
			return
		}
		form := url.Values{"invitation_cookie": {invitationState(ctx)}}
		cookie, location, err := s.StartProvider(ctx.Request.Context(), ctx.Param("provider"), "invitation", form)
		if err != nil {
			invitationFailure(ctx, err)
			return
		}
		setProviderCookie(ctx, cookie, 600)
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("Referrer-Policy", "no-referrer")
		ctx.Redirect(http.StatusSeeOther, location)
	}
}

func providerCallback(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !providerRate(ctx, s, "login") {
			return
		}
		provider := ctx.Param("provider")
		cookie, _ := ctx.Cookie(providerCookie)
		result, err := s.FinishProvider(ctx.Request.Context(), provider, cookie, ctx.Request.URL.Query())
		if err != nil || result.Outcome != server.ProviderAccountName {
			// The transaction is complete. Only the account-name step reuses it.
			setProviderCookie(ctx, "", -1)
		}
		if err != nil {
			providerFailure(ctx, provider, result, err)
			return
		}
		providerContinue(ctx, s, result)
	}
}

// providerContinue shows the next step of the Exp-Bench flow after a provider
// callback or a confirmation.
func providerContinue(ctx *gin.Context, s *server.Service, result server.ProviderResult) {
	label := providerLabel(result.Provider)
	switch result.Outcome {
	case server.ProviderAuthenticated:
		purpose := result.Path()
		authorizeBrowser(ctx, s, purpose == "/oauth/login", purpose == "/oauth/device", result.Form, purpose)
	case server.ProviderAccountName:
		renderOAuthPage(ctx, 200, "provider-account", oauthPage{Title: "Create account", WebLogin: true, Provider: result.Provider})
	case server.ProviderConfirmationSent:
		renderOAuthPage(ctx, 200, "complete", oauthPage{WebLogin: true, Title: "Check your email", Message: "If this " + label + " account can create an Exp-Bench account or link to an existing user, we sent a confirmation link to the email address. The link expires in 30 minutes. No account or sign-in method changes until you open the link. If you do not have an Exp-Bench user, create an account.", Retry: "/oauth/register", RetryLabel: "Create account"})
	case server.ProviderInvitation:
		if result.InvitationCookie != "" {
			setInvitationCookie(ctx, result.InvitationCookie, 1800)
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.Redirect(http.StatusSeeOther, oauthRetryFor("/oauth/invitations/authorize", result.Form))
	case server.ProviderExistingUser:
		renderOAuthPage(ctx, 200, "complete", oauthPage{WebLogin: true, Title: "User already exists", Message: "This " + label + " account already belongs to an Exp-Bench user. Log in with " + label + ".", Retry: "/sign-in", RetryLabel: "Log in"})
	case server.ProviderRegistered:
		renderOAuthPage(ctx, 200, "complete", oauthPage{WebLogin: true, Title: "Account created", Message: "Your account is ready. Log in with " + label + ".", Retry: "/sign-in", RetryLabel: "Log in"})
	default:
		renderOAuthPage(ctx, 200, "complete", oauthPage{WebLogin: true, Title: label + " sign-in linked", Message: "You can now log in with " + label + ".", Retry: "/sign-in", RetryLabel: "Log in"})
	}
}

// providerFailure reports a provider or transaction failure. The restart
// action starts a new bounded transaction from the originating page.
func providerFailure(ctx *gin.Context, provider string, result server.ProviderResult, err error) {
	status, message := http.StatusInternalServerError, "The service cannot complete sign-in. Try again."
	var p *server.Problem
	if errors.As(err, &p) {
		status, message = p.Status, p.Detail
	}
	retry := "/sign-in"
	if result.Form.Get("client_id") != "" || result.Form.Get("user_code") != "" || result.Purpose == "register" {
		retry = oauthRetryFor(result.Path(), result.Form)
	}
	title := providerLabel(provider) + " sign-in stopped"
	if p != nil && p.Code == "mail-unavailable" {
		title = "Account email unavailable"
	}
	renderOAuthPage(ctx, status, "complete", oauthPage{WebLogin: true, Title: title, Message: message, Failed: true, Retry: retry, RetryLabel: "Return to sign-in"})
}

func providerRegistration(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		cookie, _ := ctx.Cookie(providerCookie)
		provider, err := s.PendingProviderRegistration(ctx.Request.Context(), cookie)
		if err != nil {
			setProviderCookie(ctx, "", -1)
			providerFailure(ctx, provider, server.ProviderResult{Purpose: "register"}, err)
			return
		}
		name := ctx.PostForm("account_name")
		result, err := s.SubmitProviderAccountName(ctx.Request.Context(), cookie, name)
		if err != nil {
			var p *server.Problem
			if errors.As(err, &p) && (p.Code == "validation" || p.Code == "account-name-in-use") {
				// Keep the transaction and the input so the person can correct the name.
				renderOAuthPage(ctx, p.Status, "provider-account", oauthPage{Title: "Create account", WebLogin: true, Provider: provider, AccountName: name, Failed: true, Message: p.Detail, Fields: p.Fields})
				return
			}
			setProviderCookie(ctx, "", -1)
			providerFailure(ctx, provider, result, err)
			return
		}
		setProviderCookie(ctx, "", -1)
		providerContinue(ctx, s, result)
	}
}

func providerConfirmation(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := ctx.Request.ParseForm(); err != nil {
			renderIdentityPage(ctx, 400, "complete", oauthPage{Title: "Invalid request", Message: "The service cannot read this form. Try again.", Failed: true, Retry: "/sign-in"})
			return
		}
		token := ctx.Request.Form.Get("token")
		if ctx.Request.Method == http.MethodGet {
			detail, err := s.ProviderConfirmation(ctx.Request.Context(), token)
			if err != nil {
				renderIdentityPage(ctx, 400, "complete", oauthPage{Title: "Link unavailable", Message: "This link is invalid, expired, or already used. Start sign-in again to request a new link.", Failed: true, Retry: "/sign-in"})
				return
			}
			renderOAuthPage(ctx, 200, "provider-confirm", oauthPage{Title: "Confirm sign-in", WebLogin: true, Token: token, Provider: detail.Provider, Confirmation: &detail})
			return
		}
		result, err := s.CompleteProviderConfirmation(ctx.Request.Context(), token)
		if err != nil {
			var p *server.Problem
			if errors.As(err, &p) && p.Code == "invalid-link" {
				renderIdentityPage(ctx, 400, "complete", oauthPage{Title: "Link unavailable", Message: "This link is invalid, expired, or already used. Start sign-in again to request a new link.", Failed: true, Retry: "/sign-in"})
				return
			}
			providerFailure(ctx, result.Provider, server.ProviderResult{}, err)
			return
		}
		providerContinue(ctx, s, result)
	}
}

const ProviderCookie = providerCookie
