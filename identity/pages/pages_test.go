package pages

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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

// An undeclared custom property resolves to nothing, so a page would silently
// lose a colour rather than fail. Every var() the pages use must have a default.
func TestEveryThemeTokenHasADefault(t *testing.T) {
	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+):`).FindAllStringSubmatch(oauthStyle, -1) {
		declared[m[1]] = true
	}
	used := regexp.MustCompile(`var\((--[a-z0-9-]+)\)`).FindAllStringSubmatch(oauthStyle, -1)
	if len(used) == 0 {
		t.Fatal("the pages use no theme tokens")
	}
	for _, m := range used {
		if !declared[m[1]] {
			t.Errorf("%s is used but has no default", m[1])
		}
	}
}

// A product's wording replaces the default on every page that carries product
// wording, and a field the product leaves empty keeps the default.
func TestProductCopyReplacesTheDefault(t *testing.T) {
	urth := Copy{
		Tagline:               "Synthetic monitoring",
		Headline:              "Watch every network.",
		AuthorizeHeadline:     "Connect to your probes.",
		Description:           "Run scenarios where your services live.",
		InvitationDescription: "Join a monitoring account.",
		MachineTokens:         "runner tokens",
	}
	defaults := []string{DefaultCopy.Tagline, DefaultCopy.Headline, DefaultCopy.AuthorizeHeadline, DefaultCopy.Description, DefaultCopy.InvitationDescription, DefaultCopy.MachineTokens}
	seen := map[string]bool{}
	for _, name := range []string{"login", "web-login", "account-access", "invitation"} {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		Render(ctx, 200, name, Page{Title: "T", AccountArchived: true}, Config{ProductName: "Urth", Copy: urth})
		// The script's headline riff names the default headline but acts only on it.
		body := regexp.MustCompile(`(?s)<script>.*?</script>`).ReplaceAllString(w.Body.String(), "")
		for _, d := range defaults {
			if strings.Contains(body, d) {
				t.Errorf("%s still shows the default %q", name, d)
			}
		}
		for _, v := range []string{urth.Tagline, urth.Headline, urth.AuthorizeHeadline, urth.Description, urth.InvitationDescription, urth.MachineTokens} {
			seen[v] = seen[v] || strings.Contains(body, v)
		}
	}
	for v, ok := range seen {
		if !ok {
			t.Errorf("no page shows %q", v)
		}
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	Render(ctx, 200, "web-login", Page{Title: "T"}, Config{Copy: Copy{Tagline: "Synthetic monitoring"}})
	if body := w.Body.String(); !strings.Contains(body, "Synthetic monitoring") || !strings.Contains(body, DefaultCopy.Headline) {
		t.Error("an empty field must keep the default wording")
	}
}
