package bark

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func bindParams(t *testing.T, rawQuery string) SearchParams {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)

	var params SearchParams
	require.NoError(t, ctx.ShouldBindQuery(&params))
	return params
}

func TestBuildQueryWindow(t *testing.T) {
	limits := PageLimits{Default: 100, Max: 1024}

	cases := map[string]struct {
		query          string
		preset         func(*SearchParams)
		offset, limit  uint
		cursor         string
		wantConflicted bool
	}{
		"nothing asks for the default":   {query: "", limit: 100},
		"limit and offset":               {query: "limit=20&offset=40", offset: 40, limit: 20},
		"limit is capped":                {query: "limit=5000", limit: 1024},
		"page and pageSize still work":   {query: "page=2&pageSize=25", offset: 50, limit: 25},
		"pageSize is capped before page": {query: "page=1&pageSize=5000", offset: 1024, limit: 1024},
		"cursor":                         {query: "cursor=abc&limit=10", limit: 10, cursor: "abc"},
		"cursor with offset":             {query: "cursor=abc&offset=10", wantConflicted: true},
		"cursor with page":               {query: "cursor=abc&page=1", wantConflicted: true},
		"explicit limit wins over pageSize": {
			query: "pageSize=7&limit=3", limit: 3,
		},
		// What Exp-Bench's middleware does: preset a pageSize, then expect a
		// client's offset/limit to apply.
		"preset pageSize, client offset and limit": {
			query:  "offset=10&limit=5",
			preset: func(p *SearchParams) { p.PageSize = 100 },
			offset: 10, limit: 5,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			params := bindParams(t, c.query)
			if c.preset != nil {
				c.preset(&params)
			}

			q, err := params.BuildQueryWithLimits(limits)
			if c.wantConflicted {
				require.ErrorIs(t, err, ErrConflictingPagination)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.offset, q.Offset, "offset")
			require.Equal(t, c.limit, q.Limit, "limit")
			require.Equal(t, c.cursor, q.Cursor, "cursor")
		})
	}
}

// BuildQuery keeps its meaning: one number, both the default and the cap.
func TestBuildQueryKeepsSingleLimitMeaning(t *testing.T) {
	q, err := bindParams(t, "").BuildQuery(512)
	require.NoError(t, err)
	require.EqualValues(t, 512, q.Limit)

	q, err = bindParams(t, "pageSize=9000&page=1").BuildQuery(512)
	require.NoError(t, err)
	require.EqualValues(t, 512, q.Limit)
	require.EqualValues(t, 512, q.Offset)
}

func TestBuildQueryParsesFields(t *testing.T) {
	q, err := bindParams(t, url.Values{"fields": {"metadata.project=p1"}, "labels": {"team=a"}}.Encode()).BuildQueryWithLimits(DefaultPageLimits)
	require.NoError(t, err)
	require.NotNil(t, q.Fields)
	require.True(t, q.Fields.Matches(manifest.Labels{"metadata.project": "p1"}))
	require.True(t, q.Selector.Matches(manifest.Labels{"team": "a"}))

	_, err = bindParams(t, "fields="+url.QueryEscape("a in (")).BuildQueryWithLimits(DefaultPageLimits)
	require.Error(t, err)
}

func TestPageResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	total := int64(3)

	serve := func(t *testing.T, target string, items []string, page manifest.Page) map[string]any {
		t.Helper()
		router := gin.New()
		router.GET("/items", ContentTypeAPI(), SearchableAPIWithLimits(DefaultPageLimits), func(ctx *gin.Context) {
			WithContext[string](ctx).Page(items, page, nil)
		})

		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	t.Run("middle page", func(t *testing.T) {
		body := serve(t, "/items?limit=2&labels=team%3Da&offset=4", []string{"a", "b"}, manifest.Page{Limit: 2, Next: "CUR", Total: &total})
		require.Equal(t, []any{"a", "b"}, body["items"])
		require.EqualValues(t, 2, body["limit"])
		require.Equal(t, "CUR", body["next"])
		require.EqualValues(t, 3, body["total"])

		next, err := url.Parse(body["_links"].(map[string]any)["next"].(map[string]any)["ref"].(string))
		require.NoError(t, err)
		require.Equal(t, "CUR", next.Query().Get("cursor"))
		require.Equal(t, "team=a", next.Query().Get("labels"), "the next link keeps the filter")
		require.Empty(t, next.Query().Get("offset"), "a cursor replaces the offset")
	})

	t.Run("last page", func(t *testing.T) {
		body := serve(t, "/items", nil, manifest.Page{Limit: 100})
		require.Equal(t, []any{}, body["items"], "an empty page is an empty array, not null")
		require.NotContains(t, body, "next")
		require.NotContains(t, body, "total", "an uncounted total is absent, not zero")
		require.NotContains(t, body["_links"].(map[string]any), "next")
	})
}

func TestBuildCursorQueryRefusesPositionalPaging(t *testing.T) {
	for _, query := range []string{"offset=0", "offset=10", "page=1", "pageSize=20", "cursor=abc&offset=5"} {
		_, err := bindParams(t, query).BuildCursorQuery(DefaultPageLimits)
		require.ErrorIs(t, err, ErrOffsetUnsupported, query)
	}

	q, err := bindParams(t, "cursor=abc&limit=5000&labels=team%3Da").BuildCursorQuery(DefaultPageLimits)
	require.NoError(t, err)
	require.Equal(t, "abc", q.Cursor)
	require.EqualValues(t, 1024, q.Limit)
	require.Zero(t, q.Offset)
}

func TestCursorListingErrorsAreProblems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/items", ContentTypeAPI(), CursorSearchableAPI(DefaultPageLimits), func(ctx *gin.Context) {
		var err error
		switch ctx.Query("fail") {
		case "cursor":
			err = manifest.NewStatusError(http.StatusBadRequest, "invalid-cursor", "invalid page cursor")
		case "store":
			err = errors.New("connection reset")
		}
		WithContext[string](ctx).Page(nil, manifest.Page{Limit: 100}, err)
	})

	problem := func(t *testing.T, target string) (int, Problem) {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, MimeTypeProblemJSON, w.Header().Get(HTTPHeaderContentType), w.Body.String())
		var p Problem
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
		return w.Code, p
	}

	code, p := problem(t, "/items?offset=20")
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "offset-unsupported", p.Code)

	code, p = problem(t, "/items?fail=cursor")
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "invalid-cursor", p.Code, "an error that carries a status keeps it")

	code, p = problem(t, "/items?fail=store")
	require.Equal(t, http.StatusInternalServerError, code, "a failure to list is not the client's fault")
	require.Equal(t, "internal-server-error", p.Code)
}
