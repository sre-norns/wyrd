package httpapi

import (
	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	"mime"
	"net/http"
)

func resourceContentType() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Request.Method == http.MethodPost || ctx.Request.Method == http.MethodPatch || ctx.Request.Method == http.MethodPut {
			if ctx.Request.ContentLength != 0 {
				media, _, err := mime.ParseMediaType(ctx.GetHeader("Content-Type"))
				if err != nil || (media != "application/json" && !(media == "application/merge-patch+json" && ctx.Request.Method == http.MethodPatch)) {
					writeProblem(ctx, &server.Problem{Status: 415, Code: "unsupported-media-type", Detail: "Resource requests require application/json; PATCH also accepts application/merge-patch+json."})
					return
				}
			}
		}
		ctx.Next()
	}
}
