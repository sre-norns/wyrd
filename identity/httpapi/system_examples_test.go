package httpapi

import (
	"bytes"
	"github.com/gin-gonic/gin"
	e "github.com/sre-norns/wyrd/identity/model"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSystemCommandExamplesUseMountedParser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, example := range []struct {
		name   string
		parser gin.HandlerFunc
	}{
		{"system-account-create.json", ResourceValueAPI[e.SystemAccountCreate]()},
		{"owner-recovery-create.json", ResourceValueAPI[e.SystemAction]()},
		{"deletion-request-create.json", ResourceValueAPI[e.SystemAction]()},
	} {
		t.Run(example.name, func(t *testing.T) {
			data, err := os.ReadFile("../examples/" + example.name)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/command", example.parser, func(ctx *gin.Context) { ctx.Status(204) })
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("POST", "/command", bytes.NewReader(data)))
			if w.Code != 204 {
				t.Fatalf("command example: %d %s", w.Code, w.Body)
			}
			// Explicit commands do not accept canonical read documents or arbitrary fields.
			for _, bad := range []string{`{"apiVersion":"identity.sre-norns.com/v1","kind":"system-accounts"}`, `{"unknown":true}`, `{} {}`} {
				w = httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest("POST", "/command", bytes.NewBufferString(bad)))
				if w.Code < 400 {
					t.Fatalf("accepted invalid command: %s", bad)
				}
			}
		})
	}
}
