package bark_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/stretchr/testify/require"
)

func TestAuthBearerAPIRejectsMissingTokenWithoutPanicking(t *testing.T) {
	router := gin.New()
	downstreamCalled := false
	router.Use(bark.AuthBearerAPI())
	router.GET("/", func(ctx *gin.Context) {
		downstreamCalled = true
		ctx.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(bark.HTTPHeaderAuth, "Bearer")
	response := httptest.NewRecorder()

	require.NotPanics(t, func() {
		router.ServeHTTP(response, request)
	})
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, "Bearer", response.Header().Get("WWW-Authenticate"))
	require.False(t, downstreamCalled)
}

func TestParseBearerAcceptsCaseInsensitiveScheme(t *testing.T) {
	token, err := bark.ParseBearer("bEaReR opaque-token")

	require.NoError(t, err)
	require.Equal(t, "opaque-token", token)
}

func TestParseBearerAcceptsMultipleCredentialSpaces(t *testing.T) {
	token, err := bark.ParseBearer("Bearer   opaque-token")

	require.NoError(t, err)
	require.Equal(t, "opaque-token", token)
}

func TestParseBearerPreservesRFC6750Token(t *testing.T) {
	for _, token := range []string{
		"a",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"abcdefghijklmnopqrstuvwxyz",
		"0123456789",
		"opaque._~+/-token",
		"opaque-token====",
	} {
		t.Run(token, func(t *testing.T) {
			got, err := bark.ParseBearer("Bearer " + token)

			require.NoError(t, err)
			require.Equal(t, token, got)
		})
	}
}

func TestParseBearerClassifiesMissingHeader(t *testing.T) {
	token, err := bark.ParseBearer("")

	require.Empty(t, token)
	requireBearerParseError(t, err, bark.BearerParseHeaderMissing)
}

func TestParseBearerClassifiesWrongScheme(t *testing.T) {
	token, err := bark.ParseBearer("Basic opaque-token")

	require.Empty(t, token)
	requireBearerParseError(t, err, bark.BearerParseSchemeInvalid)
}

func TestParseBearerClassifiesMissingToken(t *testing.T) {
	for _, header := range []string{"Bearer", "Bearer ", "Bearer    "} {
		t.Run(header, func(t *testing.T) {
			token, err := bark.ParseBearer(header)

			require.Empty(t, token)
			requireBearerParseError(t, err, bark.BearerParseTokenMissing)
		})
	}
}

func TestParseBearerRejectsExtraCredentialFields(t *testing.T) {
	for _, header := range []string{
		"Bearer opaque-token extra",
		"Bearer opaque-token ",
		"Bearer opaque-token\textra",
	} {
		t.Run(header, func(t *testing.T) {
			token, err := bark.ParseBearer(header)

			require.Empty(t, token)
			requireBearerParseError(t, err, bark.BearerParseCredentialsExtra)
		})
	}
}

func TestParseBearerRejectsMalformedTokens(t *testing.T) {
	for _, header := range []string{
		"Bearer opaque-token,other",
		"Bearer tøken",
		"Bearer opaque\x00token",
		"Bearer opaque=token",
		"Bearer =",
		"Bearer opaque:token",
		"Bearer\topaque-token",
	} {
		t.Run(header, func(t *testing.T) {
			token, err := bark.ParseBearer(header)

			require.Empty(t, token)
			requireBearerParseError(t, err, bark.BearerParseTokenInvalid)
		})
	}
}

func requireBearerParseError(t *testing.T, err error, kind bark.BearerParseErrorKind) {
	t.Helper()

	var parseError *bark.BearerParseError
	require.ErrorAs(t, err, &parseError)
	require.Equal(t, kind, parseError.Kind())
	require.True(t, errors.Is(err, bark.ErrInvalidAuthHeader))
}

func TestAuthBearerAPIUsesOneSafeExternalFailure(t *testing.T) {
	var expectedBody string
	for _, header := range []string{
		"",
		"Basic super-secret",
		"Bearer super-secret,other",
	} {
		t.Run(header, func(t *testing.T) {
			router := gin.New()
			var logs bytes.Buffer
			router.Use(gin.LoggerWithWriter(&logs))
			router.Use(bark.AuthBearerAPI())
			router.GET("/", func(ctx *gin.Context) {
				ctx.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set(bark.HTTPHeaderAuth, header)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			require.Equal(t, http.StatusUnauthorized, response.Code)
			require.Equal(t, "Bearer", response.Header().Get(bark.HTTPHeaderWWWAuthenticate))
			require.NotContains(t, response.Body.String(), "super-secret")
			require.NotContains(t, logs.String(), "super-secret")
			if expectedBody == "" {
				expectedBody = response.Body.String()
			} else {
				require.Equal(t, expectedBody, response.Body.String())
			}
		})
	}
}

func TestAuthBearerAPIPropagatesValidatedToken(t *testing.T) {
	router := gin.New()
	var receivedToken string
	router.Use(bark.AuthBearerAPI())
	router.GET("/", func(ctx *gin.Context) {
		receivedToken = bark.RequireBearerToken(ctx)
		ctx.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(bark.HTTPHeaderAuth, "bEaReR opaque._~+/-token==")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, "opaque._~+/-token==", receivedToken)
	require.Empty(t, response.Header().Get(bark.HTTPHeaderWWWAuthenticate))
}

type countingResponseWriter struct {
	http.ResponseWriter
	headerWrites int
	bodyWrites   int
}

func (w *countingResponseWriter) WriteHeader(statusCode int) {
	w.headerWrites++
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *countingResponseWriter) Write(body []byte) (int, error) {
	w.bodyWrites++
	return w.ResponseWriter.Write(body)
}

func TestAuthBearerAPIWritesOneFailureResponse(t *testing.T) {
	router := gin.New()
	router.Use(bark.AuthBearerAPI())
	router.GET("/", func(ctx *gin.Context) {
		ctx.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(bark.HTTPHeaderAuth, "Bearer")
	recorder := httptest.NewRecorder()
	response := &countingResponseWriter{ResponseWriter: recorder}
	router.ServeHTTP(response, request)

	require.Equal(t, 1, response.headerWrites)
	require.Equal(t, 1, response.bodyWrites)
}

func FuzzParseBearer(f *testing.F) {
	for _, header := range []string{
		"",
		"Bearer",
		"Bearer ",
		"Bearer opaque-token",
		"bEaReR opaque._~+/-token==",
		"Basic opaque-token",
		"Bearer opaque-token extra",
		"Bearer opaque-token,other",
		"Bearer\topaque-token",
		"Bearer tøken",
		"Bearer opaque\x00token",
	} {
		f.Add(header)
	}

	f.Fuzz(func(t *testing.T, header string) {
		token, err := bark.ParseBearer(header)
		if err != nil {
			require.Empty(t, token)
			require.ErrorIs(t, err, bark.ErrInvalidAuthHeader)
			return
		}

		require.NotEmpty(t, token)
		reparsed, err := bark.ParseBearer("Bearer " + token)
		require.NoError(t, err)
		require.Equal(t, token, reparsed)
	})
}
