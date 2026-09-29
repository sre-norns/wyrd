package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

func TestSearchablePagesByCursorOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var got manifest.SearchQuery
	router.GET("/items", searchable(), func(ctx *gin.Context) {
		got = requireSearchQuery(ctx)
		total := int64(3)
		response[string](ctx).List([]string{"a"}, manifest.Page{Limit: got.Limit, Next: "n", Total: &total}, nil)
	})
	get := func(target string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		return w
	}

	for _, target := range []string{"/items?offset=0", "/items?page=2", "/items?pageSize=10", "/items?cursor=c&offset=3"} {
		w := get(target)
		var problem map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || w.Code != 400 || problem["code"] != "offset-unsupported" || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
	}

	w := get("/items?cursor=c&limit=5000")
	if w.Code != 200 || got.Cursor != "c" || got.Limit != 1024 {
		t.Fatalf("%d %+v", w.Code, got)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{"offset"} {
		if _, ok := body[stale]; ok {
			t.Fatalf("list response still carries %q: %v", stale, body)
		}
	}
	if body["next"] != "n" || body["limit"] != float64(1024) || body["total"] != float64(3) {
		t.Fatalf("list response: %v", body)
	}

	if get("/items").Code != 200 || got.Limit != 100 {
		t.Fatalf("default limit %d", got.Limit)
	}
}
