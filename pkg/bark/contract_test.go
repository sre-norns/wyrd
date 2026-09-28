package bark

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestProblemFromError(t *testing.T) {
	sentinel := manifest.NewStatusError(http.StatusNotFound, "not-found", "gone")

	cases := map[string]struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		"status error":         {sentinel, http.StatusNotFound, "not-found"},
		"wrapped status error": {fmt.Errorf("loading scenario: %w", sentinel), http.StatusNotFound, "not-found"},
		"scope mismatch":       {fmt.Errorf("apply: %w", manifest.ErrScopeMismatch), http.StatusBadRequest, "scope-mismatch"},
		"legacy ErrorResponse": {ErrResourceVersionConflict, http.StatusConflict, "conflict"},
		"plain error":          {errors.New("boom"), http.StatusInternalServerError, "internal-server-error"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p := ProblemFromError(http.StatusInternalServerError, c.err)
			require.Equal(t, c.wantStatus, p.Status)
			require.Equal(t, c.wantCode, p.Code)
			require.Equal(t, c.err.Error(), p.Detail, "the detail keeps the whole chain")
			require.Equal(t, http.StatusText(c.wantStatus), p.Title)
		})
	}

	require.Nil(t, ProblemFromError(http.StatusBadRequest, nil))
}

// A Problem declared once and returned from many requests must not carry one
// request's instance into another's response.
func TestAbortWithProblemDoesNotMutateSentinel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sentinel := &Problem{Title: "Teapot", Status: http.StatusTeapot, Code: "teapot"}

	router := gin.New()
	router.GET("/a", func(ctx *gin.Context) { AbortWithProblem(ctx, http.StatusBadRequest, sentinel) })

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/a", nil))

	require.Equal(t, http.StatusTeapot, w.Code)
	require.Equal(t, MimeTypeProblemJSON, w.Header().Get(HTTPHeaderContentType))
	var body Problem
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "/a", body.Instance)
	require.Equal(t, "teapot", body.Code)
	require.Empty(t, sentinel.Instance)
}

func TestParseIfMatch(t *testing.T) {
	cases := map[string]struct {
		version manifest.Version
		any     bool
		err     error
	}{
		``:          {err: ErrPreconditionRequired},
		`"3"`:       {version: 3},
		` "42" `:    {version: 42},
		`*`:         {any: true},
		`W/"3"`:     {err: ErrPreconditionFailed},
		`3`:         {err: ErrMalformedIfMatch},
		`"0"`:       {err: ErrMalformedIfMatch},
		`"x"`:       {err: ErrMalformedIfMatch},
		`"1", "2"`:  {err: ErrMalformedIfMatch},
		`"-1"`:      {err: ErrMalformedIfMatch},
		`"3" extra`: {err: ErrMalformedIfMatch},
	}
	for value, c := range cases {
		t.Run(value, func(t *testing.T) {
			version, any, err := ParseIfMatch(value)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.version, version)
			require.Equal(t, c.any, any)
		})
	}
}

func TestRequireIfMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ContentTypeAPI(), RequireIfMatch())
	handler := func(ctx *gin.Context) {
		version, ok := IfMatchVersion(ctx)
		ctx.String(http.StatusOK, "%d %v", version, ok)
	}
	router.GET("/r", handler)
	router.PUT("/r", handler)
	router.DELETE("/r", handler)

	do := func(method, ifMatch string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/r", nil)
		if ifMatch != "" {
			req.Header.Set(HTTPHeaderIfMatch, ifMatch)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, "0 false", do(http.MethodGet, "").Body.String(), "reads need no precondition")

	missing := do(http.MethodPut, "")
	require.Equal(t, http.StatusPreconditionRequired, missing.Code)
	require.Contains(t, missing.Body.String(), `"code":"precondition-required"`)

	require.Equal(t, http.StatusBadRequest, do(http.MethodDelete, "3").Code)
	require.Equal(t, "7 true", do(http.MethodPut, `"7"`).Body.String())
	require.Equal(t, "0 false", do(http.MethodDelete, "*").Body.String(), "* asks for no version check")
}

type versionedThing struct {
	manifest.ObjectMeta
}

func TestFoundSetsETag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ContentTypeAPI())
	router.GET("/thing", func(ctx *gin.Context) {
		WithContext[versionedThing](ctx).Found(versionedThing{ObjectMeta: manifest.ObjectMeta{Name: "x", Version: 5}}, true, nil)
	})
	router.GET("/manifest", func(ctx *gin.Context) {
		Manifest(ctx).Found(manifest.ResourceManifest{Metadata: manifest.ObjectMeta{Version: 9}}, true, nil)
	})

	for path, want := range map[string]string{"/thing": `"5"`, "/manifest": `"9"`} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, want, w.Header().Get(HTTPHeaderETag), path)

		// The header round-trips through ParseIfMatch.
		version, _, err := ParseIfMatch(w.Header().Get(HTTPHeaderETag))
		require.NoError(t, err)
		require.True(t, strings.Contains(want, version.String()))
	}
}
