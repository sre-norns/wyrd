package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/identity/pages"
)

type oauthPage = pages.Page

var oauthRetryFor = pages.RetryFor

func renderOAuthPage(ctx *gin.Context, status int, name string, page oauthPage) {
	cfg, _ := ctx.Get("identity.presentation")
	c, _ := cfg.(Config)
	brand := c.ProductName
	if brand == "" {
		brand = "SRE-Norns"
	}
	page.Message = strings.ReplaceAll(page.Message, "Exp-Bench", brand)
	pages.Render(ctx, status, name, page, pages.Config{ProductName: c.ProductName, PrivacyURL: c.PrivacyURL, ThemeCSS: c.ThemeCSS})
}
