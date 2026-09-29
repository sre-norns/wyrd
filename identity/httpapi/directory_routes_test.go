package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
)

// The project access read models are served by Mount, so every product that
// mounts identity has them. They are behind authentication like the rest of it.
func TestMountServesDirectoryRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Mount(router, server.NewService(nil), Config{})

	registered := map[string]bool{}
	for _, r := range router.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	paths := []string{
		"/v1/projects/p/member-candidates?q=a",
		"/v1/projects/p/agent-candidates",
		"/v1/agent-identities/a/project-authorizations",
	}
	for _, route := range []string{
		"GET /v1/projects/:id/member-candidates",
		"GET /v1/projects/:id/agent-candidates",
		"GET /v1/agent-identities/:id/project-authorizations",
	} {
		if !registered[route] {
			t.Errorf("Mount does not serve %s", route)
		}
	}
	for _, path := range paths {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("anonymous %s: %d %s", path, w.Code, w.Body)
		}
	}
}
