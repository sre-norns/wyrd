package bark

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterFlags(t *testing.T) {
	testCases := map[string]struct {
		given  string
		expect string
	}{
		"empty": {
			given:  "",
			expect: "",
		},

		"identity": {
			given:  "hello",
			expect: "hello",
		},

		"case": {
			given:  "type; utf-3",
			expect: "type",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := filterFlags(test.given)

			require.Equal(t, test.expect, got)
		})
	}
}

func TestSearchableAPIRejectsUnbindableQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	reached := false
	router.GET("/", SearchableAPI(10), func(ctx *gin.Context) {
		reached = true
		ctx.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?page=not-a-number", nil))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.False(t, reached, "handler must not run after a rejected query")

	var body ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "response must be exactly one JSON document: %s", w.Body.String())
}
