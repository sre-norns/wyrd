package bark

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

const (
	responseMarshalKey = "responseMarshal"
	searchQueryKey     = "searchQuery"
	resourceIDKey      = "resourceId"
	versionedIDKey     = "versionedId"
	versionInfoKey     = "versionInfoKey"

	resourceManifestKey = "resourceManifestKey"

	authBearerKey = "Bearer"

	// well known HTTP headers
	HttHeaderAuth         = "Authorization"
	HttHeaderAccept       = "Accept"
	HttHeaderLocation     = "Location"
	HttHeaderContentType  = "Content-Type"
	HttHeaderCacheControl = "Cache-Control"
)

var (
	ErrUnsupportedMediaType = fmt.Errorf("unsupported content type request")
	ErrInvalidAuthHeader    = fmt.Errorf("invalid Authorization header")
	ErrWrongKind            = fmt.Errorf("invalid resource kind for the API")
)

// Lifted from GIN
func filterFlags(content string) string {
	for i, char := range content {
		if char == ' ' || char == ';' {
			return content[:i]
		}
	}
	return content
}

// Get a list of accepted MIME-data types from request headers
func selectAcceptedType(header http.Header) []string {
	accepts := header.Values(HttHeaderAccept)
	result := make([]string, 0, len(accepts))
	for _, a := range accepts {
		result = append(result, filterFlags(a))
	}

	return result
}

// responseHandler is type to represent functions that can process response object
type responseHandler func(code int, obj any)

func replyWithAcceptedType(c *gin.Context) (responseHandler, error) {
	for _, contentType := range selectAcceptedType(c.Request.Header) {
		switch contentType {
		case "", "*/*", gin.MIMEJSON:
			return c.JSON, nil
		case gin.MIMEYAML, "text/yaml", "application/yaml", "text/x-yaml":
			return c.YAML, nil
		case gin.MIMEXML, gin.MIMEXML2:
			return c.XML, nil
		}
	}

	return nil, ErrUnsupportedMediaType
}

// MarshalResponse selects appropriate resource handler based on `Accept` request headers and marshals response object.
func MarshalResponse(ctx *gin.Context, code int, responseValue any) {
	marshalResponse := ctx.MustGet(responseMarshalKey).(responseHandler)
	marshalResponse(code, responseValue)
}

// ReplyResourceCreated is a shortcut to handle 201/Created response. It adds proper `Location` header
func ReplyResourceCreated(ctx *gin.Context, id any, resource any) {
	ctx.Header(HttHeaderLocation, fmt.Sprintf("%v/%v", ctx.Request.URL.Path, id))
	MarshalResponse(ctx, http.StatusCreated, resource)
}

// AbortWithError terminates response-handling chain with an error, and returns provided HTTP error response to a client
func AbortWithError(ctx *gin.Context, code int, errValue error) {
	if apiError, ok := errValue.(*ErrorResponse); ok {
		ctx.AbortWithStatusJSON(apiError.Code, apiError)
		return
	}

	ctx.AbortWithStatusJSON(code, NewErrorResponse(code, errValue))
}

// ContentTypeAPI is a filter/middleware to add support response marshaler selection based on `Accept` header.
// Used in conjunction with `MarshalResponse` and `ReplyResourceCreated`
func ContentTypeAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// select response encoder base of accept-type:
		marshalResponse, err := replyWithAcceptedType(ctx)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest, NewErrorResponse(http.StatusBadRequest, err))
			return
		}

		ctx.Set(responseMarshalKey, marshalResponse)
		ctx.Next()
	}
}

// SearchableAPI is a filter/middleware to add support for `SearchQuery` parameter.
// See `RequireSearchQuery` usage how to obtain `SearchQuery` object in the request handler
func SearchableAPI(defaultPaginationLimit uint) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var searchQuery SearchQuery
		if ctx.ShouldBindQuery(&searchQuery) != nil {
			searchQuery.Limit = defaultPaginationLimit
		}

		searchQuery.Pagination = searchQuery.ClampLimit(defaultPaginationLimit)
		ctx.Set(searchQueryKey, searchQuery)
		ctx.Next()
	}
}

func RequireSearchQuery(ctx *gin.Context) SearchQuery {
	return ctx.MustGet(searchQueryKey).(SearchQuery)
}

func extractAuthBearer(ctx *gin.Context) (string, error) {
	// Get the "Authorization" header
	authorization := ctx.Request.Header.Get(HttHeaderAuth)
	if authorization == "" {
		return "", ErrInvalidAuthHeader
	}

	// Split it into two parts - "Bearer" and token
	parts := strings.SplitN(authorization, " ", 2)
	if parts[0] != "Bearer" {
		return "", ErrInvalidAuthHeader
	}

	return parts[1], nil
}

// AuthBearerAPI is a filter/middleware that extracts "Bearer" token from an incoming request headers
// See `RequireBearerToken` for info on how to access the token.
func AuthBearerAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, err := extractAuthBearer(ctx)
		if err != nil {
			AbortWithError(ctx, http.StatusUnauthorized, err)
			return
		}

		ctx.Set(authBearerKey, token)
		ctx.Next()
	}
}

// RequireBearerToken - returns previously extracted "Bearer" token from a request header.
// Note: It requires AuthBearerAPI() middleware to be in the request handler call-chain.
func RequireBearerToken(ctx *gin.Context) string {
	return ctx.MustGet(authBearerKey).(string)
}

// Monkey-patch GIN to respect other spelling of yaml mime-type
func bindingFor(method, contentType string) binding.Binding {
	switch contentType {
	case gin.MIMEYAML, "text/yaml", "application/yaml", "text/x-yaml":
		return binding.YAML
	case "", "*/*", gin.MIMEJSON:
		return binding.JSON
	default:
		return binding.Default(method, contentType)
	}
}
