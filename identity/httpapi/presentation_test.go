package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/identity/pages"
)

// Every page setting a product mounts with reaches the pages Mount serves.
// v0.4.0 rebuilt the pages' Config from three named fields and dropped Copy,
// so a product's wording never appeared -- while pages.Render's own tests,
// which bypass Mount, passed.
func TestMountedPagesCarryTheProductsPresentation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	cfg := Config{
		ProductName: "Urth",
		PrivacyURL:  "/privacy-notice",
		ThemeCSS:    ":root{--primary-container:#22c55e}",
		Copy: pages.Copy{
			Tagline:           "Synthetic monitoring",
			AuthorizeHeadline: "Connect to your runners.",
			Description:       "Probe services from the networks they live in.",
			HeadlineVariants:  []string{"See it from inside.", "Probe it from inside."},
		},
	}
	Mount(router, server.NewService(nil), cfg)
	// Registered after Mount, so Mount's presentation middleware applies, as it
	// does to every page route Mount registers.
	router.GET("/presentation-probe/:page", func(ctx *gin.Context) {
		renderOAuthPage(ctx, http.StatusOK, ctx.Param("page"), oauthPage{Title: "T", ClientID: "urth-web"})
	})

	for page, want := range map[string][]string{
		"login":     {"Connect to your runners.", "Probe services from the networks they live in.", "Synthetic monitoring", "/privacy-notice", "--primary-container:#22c55e"},
		"web-login": {"See it from inside.", "Probe it from inside.", "Synthetic monitoring"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/presentation-probe/"+page, nil))
		body := w.Body.String()
		for _, text := range want {
			if !strings.Contains(body, text) {
				t.Errorf("%s: the mounted page lacks %q", page, text)
			}
		}
		for _, text := range []string{pages.DefaultCopy.Tagline, pages.DefaultCopy.AuthorizeHeadline} {
			if strings.Contains(body, text) {
				t.Errorf("%s: the mounted page still shows the default %q", page, text)
			}
		}
	}
}

// An expired device sign-in names the product's own tool. The message was
// hard-coded to expbctl, so Urth told its operators to run another product's CLI.
func TestExpiredDeviceSignInNamesTheProductsTool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		copy pages.Copy
		want string
	}{
		{pages.Copy{DeviceRetry: "Run urthctl auth login again."}, "Run urthctl auth login again."},
		{pages.Copy{}, pages.DefaultCopy.DeviceRetry},
	} {
		router := gin.New()
		Mount(router, server.NewService(nil), Config{ProductName: "Urth", Copy: tc.copy})
		router.GET("/device-probe", func(ctx *gin.Context) {
			expired := &server.Problem{Status: http.StatusBadRequest, Code: "expired_token", Detail: "The device request expired."}
			providerFailure(ctx, "github", server.ProviderResult{Purpose: "device", Form: url.Values{"user_code": {"ABCDEFGHIJKL"}}}, expired)
		})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/device-probe", nil))
		body := w.Body.String()
		if !strings.Contains(body, "The device request expired. "+tc.want) {
			t.Errorf("the page lacks %q", tc.want)
		}
		if tc.copy.DeviceRetry != "" && strings.Contains(body, "expbctl") {
			t.Error("the page names another product's tool")
		}
	}
}
