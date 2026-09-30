package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/identity/pages"
)

type oauthPage = pages.Page

var oauthRetryFor = pages.RetryFor

// presentation is the Config Mount was given.
func presentation(ctx *gin.Context) Config {
	cfg, _ := ctx.Get("identity.presentation")
	c, _ := cfg.(Config)
	return c
}

func renderOAuthPage(ctx *gin.Context, status int, name string, page oauthPage) {
	c := presentation(ctx)
	brand := c.ProductName
	if brand == "" {
		brand = "SRE-Norns"
	}
	page.Message = strings.ReplaceAll(page.Message, "Exp-Bench", brand)
	pages.Render(ctx, status, name, page, pages.Config{ProductName: c.ProductName, PrivacyURL: c.PrivacyURL, ThemeCSS: c.ThemeCSS})
}
