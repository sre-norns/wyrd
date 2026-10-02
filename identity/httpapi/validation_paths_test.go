package httpapi

import (
	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/identity/model"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidationPathUsesEditedResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PATCH("/profile", ResourceValueAPI[model.PersonalProfile](), func(ctx *gin.Context) {
		writeProblem(ctx, &server.Problem{Status: 422, Code: "validation", Fields: map[string]string{"display_name": "invalid name"}})
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("PATCH", "/profile", strings.NewReader(`{"spec":{"displayName":"   "}}`))
	router.ServeHTTP(recorder, request)
	if recorder.Code != 422 || !strings.Contains(recorder.Body.String(), `"spec.displayName":"invalid name"`) {
		t.Fatalf("profile validation: %d %s", recorder.Code, recorder.Body)
	}
}
