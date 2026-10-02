package bark_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestResourceErrorsKeepStatusAndHideInternalDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []int{401, 403, 404, 409, 412, 422, 428, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			router := gin.New()
			router.GET("/item", func(ctx *gin.Context) {
				ctx.Set("requestID", "request-1")
				bark.AbortResourceError(ctx, fmt.Errorf("private-wrapper-detail: %w", manifest.NewStatusError(status, "domain-code", "rejected")))
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/item", nil))
			require.Equal(t, status, w.Code)
			require.NotContains(t, w.Body.String(), "private-wrapper-detail")
			require.Equal(t, bark.MimeTypeProblemJSON, w.Header().Get("Content-Type"))
			var p bark.Problem
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
			require.Equal(t, status, p.Status)
			require.Equal(t, "domain-code", p.Code)
			require.Equal(t, "request-1", p.RequestID)
			require.Equal(t, "/item", p.Instance)
		})
	}
	for _, err := range []error{errors.New("postgres password=private-value"), &bark.Problem{Status: 500, Detail: "private-value", Fields: map[string]string{"secret": "private-value"}}, manifest.NewStatusError(200, "bad-status", "private-value")} {
		ctx, w := testProblemContext()
		bark.AbortResourceError(ctx, err)
		require.Equal(t, 500, w.Code)
		require.NotContains(t, w.Body.String(), "private-value")
		require.Contains(t, w.Body.String(), `"code":"internal-error"`)
	}
}
func testProblemContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("GET", "/item", nil)
	return ctx, w
}
func TestResourceProblemRetainsFieldsWithoutChangingSentinel(t *testing.T) {
	sentinel := &bark.Problem{Status: 422, Code: "validation", Title: "Validation failed", Fields: map[string]string{"spec.description": "required"}}
	ctx, w := testProblemContext()
	ctx.Set("requestID", "request-2")
	bark.AbortResourceError(ctx, sentinel)
	require.Contains(t, w.Body.String(), `"spec.description":"required"`)
	require.Empty(t, sentinel.RequestID)
	require.Empty(t, sentinel.Instance)
}
