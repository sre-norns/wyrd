package bark

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/sre-norns/wyrd/pkg/manifest"
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
	// HTTPHeaderAuth is a standard [header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Authorization) to communicate authorization information
	HTTPHeaderAuth = "Authorization"

	// HTTPHeaderAccept is a standard [header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept) communicating media format expected by the client
	HTTPHeaderAccept = "Accept"

	// HTTPHeaderLocation is a standard [header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Location) returns location of a newly created resource.
	HTTPHeaderLocation = "Location"

	// HTTPHeaderContentType is a standard [header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Type) inform server of how to interpret request body.
	HTTPHeaderContentType = "Content-Type"

	// HTTPHeaderCacheControl is a standard [header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Cache-Control) inform client about caching options for the response received
	HTTPHeaderCacheControl = "Cache-Control"
)

var (
	// ErrUnsupportedMediaType error indicates that [Content-Type](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Type) passed in a client request is not support by the API / endpoint.
	ErrUnsupportedMediaType = fmt.Errorf("unsupported content type request")
	// ErrInvalidAuthHeader error indicates incorrectly former [Authorization](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Authorization) header in the message.
	ErrInvalidAuthHeader = fmt.Errorf("invalid Authorization header")
	// ErrWrongKind error indicates that [manifest.Kind] passed to an endpoint is not expected by that endpoint.
	ErrWrongKind = fmt.Errorf("invalid resource kind for the API")
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
	accepts := header.Values(HTTPHeaderAccept)
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

// MarshalResponse selects appropriate resource marshaler based on [HTTPHeaderAccept] request header value and marshals response object.
func MarshalResponse(ctx *gin.Context, code int, responseValue any) {
	marshalResponse := ctx.MustGet(responseMarshalKey).(responseHandler)
	marshalResponse(code, responseValue)
}

// ReplyResourceCreated is a shortcut to handle 201/Created response.
// It sets status code to [http.StatusCreated] and adds proper `Location` header to response headers.
func ReplyResourceCreated(ctx *gin.Context, id any, resource any) {
	ctx.Header(HTTPHeaderLocation, fmt.Sprintf("%v/%v", ctx.Request.URL.Path, id))
	MarshalResponse(ctx, http.StatusCreated, resource)
}

// AbortWithError terminates response-handling chain with an error, and returns provided HTTP error response to the client
func AbortWithError(ctx *gin.Context, code int, errValue error) {
	if apiError, ok := errValue.(*ErrorResponse); ok {
		ctx.AbortWithStatusJSON(apiError.Code, apiError)
		return
	}

	ctx.AbortWithStatusJSON(code, NewErrorResponse(code, errValue))
}

// ContentTypeAPI returns middleware to support response marshaler selection based on [HTTPHeaderAccept] value.
// Used in conjunction with [MarshalResponse] and [ReplyResourceCreated]
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

// SearchableAPI return middleware to support for [SearchQuery] parameter.
// See [RequireSearchQuery] usage on how to obtain [SearchQuery] value in the request handler
func SearchableAPI(defaultPaginationLimit uint) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var searchParams SearchParams
		if ctx.ShouldBindQuery(&searchParams) != nil {
			searchParams.PageSize = defaultPaginationLimit
		}

		searchQuery, err := searchParams.BuildQuery(defaultPaginationLimit)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest, NewErrorResponse(http.StatusBadRequest, fmt.Errorf("bad search query %w", err)))
			return
		}

		ctx.Set(searchQueryKey, searchQuery)
		ctx.Next()
	}
}

// RequireSearchQuery returns [SearchQuery] from the call context previously set by [SearchableAPI] middleware in the call chain.
// Note, the function should only be called from a handler that follows after [SearchableAPI] middleware in the filter chain.
func RequireSearchQuery(ctx *gin.Context) manifest.SearchQuery {
	return ctx.MustGet(searchQueryKey).(manifest.SearchQuery)
}

func extractAuthBearer(ctx *gin.Context) (string, error) {
	// Get the "Authorization" header
	authorization := ctx.Request.Header.Get(HTTPHeaderAuth)
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

// AuthBearerAPI return a middleware that extracts "Bearer" token from an incoming request headers.
// The middleware terminates call chain and return [http.StatusUnauthorized] to a client if there is no [HTTPHeaderAuth] headers set.
// Note the middleware does not check if the token is "valid", only that it has been set.
// See [RequireBearerToken] for information on how to access the token.
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

// RequireBearerToken returns previously extracted "Bearer" token from the request context.
// Note this function can only be called after [AuthBearerAPI] middleware in the request handler call-chain.
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
