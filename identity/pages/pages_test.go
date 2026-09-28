package pages

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrandingIsPerRenderAndCSPTracksTheme(t *testing.T) {
	render := func(c Config) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		Render(ctx, 200, "complete", Page{Title: "Welcome", Message: "Ready"}, c)
		return w
	}
	a := render(Config{ProductName: "Urth", PrivacyURL: "/privacy", ThemeCSS: ":root{--accent:green}"})
	b := render(Config{ProductName: "Exp-Bench"})
	if !strings.Contains(a.Body.String(), "Urth") || strings.Contains(a.Body.String(), "Exp-Bench") {
		t.Fatal("brand leaked across page renders")
	}
	if !strings.Contains(b.Body.String(), "Exp-Bench") || strings.Contains(b.Body.String(), ":root{--accent:green}") {
		t.Fatal("theme leaked across page renders")
	}
	if a.Header().Get("Content-Security-Policy") == b.Header().Get("Content-Security-Policy") {
		t.Fatal("theme missing from CSP hash")
	}
}
