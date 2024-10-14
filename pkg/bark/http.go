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
	responseMarshalKey   = "responseMarshal"
	searchQueryParamsKey = "searchQueryParams"
	searchQueryKey       = "searchQuery"
	resourceIDKey        = "resourceId"
	versionedIDKey       = "versionedId"
	versionInfoKey       = "versionInfoKey"

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

	// MimeTypeJSON is the mime data type for JSON payload.
	MimeTypeJSON = gin.MIMEJSON
)

var (
	// ErrInvalidAuthHeader error indicates incorrectly former [Authorization](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Authorization) header in the message.
	ErrInvalidAuthHeader = fmt.Errorf("invalid Authorization header")
	// ErrWrongKind error indicates that [manifest.Kind] passed to an endpoint is not expected by that endpoint.
	ErrWrongKind = fmt.Errorf("invalid resource kind for the API")

	// ErrResourceUnauthorized represents error response when requester is not authorized to access a resource.
	ErrResourceUnauthorized = &ErrorResponse{Code: http.StatusUnauthorized, Message: "resource access unauthorized"}
	// ErrForbidden represents error response when requester don't have permission to access given resource.
	ErrForbidden = &ErrorResponse{Code: http.StatusForbidden, Message: "forbidden"}
	// ErrResourceNotFound represents error response when requested resource not found.
	ErrResourceNotFound = &ErrorResponse{Code: http.StatusNotFound, Message: "requested resource not found"}
	// ErrResourceVersionConflict represents error response when there is a conflict between resource versions.
	ErrResourceVersionConflict = &ErrorResponse{Code: http.StatusConflict, Message: "resource version conflict"}

	// ErrNotAcceptableMediaType error indicates that value(s) of [Accept](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept) headers passed by a client in a request are not support by the API / endpoint.
	ErrNotAcceptableMediaType = &ErrorResponse{Code: http.StatusNotAcceptable, Message: "server cannot produce a response matching the list of acceptable values"}

	// ErrUnsupportedMediaType error indicates that [Content-Type](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Type) passed in a client request is not support by the API / endpoint.
	ErrUnsupportedMediaType = &ErrorResponse{Code: http.StatusUnsupportedMediaType, Message: "server refuses to accept the request because the payload format is in an unsupported format"}
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
	accepts := selectAcceptedType(c.Request.Header)
	if len(accepts) == 0 {
		return c.JSON, nil
	}

	for _, contentType := range accepts {
		switch contentType {
		case "", "*", "*/*", gin.MIMEJSON:
			return c.JSON, nil
		case gin.MIMEYAML, "text/yaml", "application/yaml", "text/x-yaml":
			return c.YAML, nil
		case gin.MIMEXML, gin.MIMEXML2:
			return c.XML, nil
		}
	}

	return nil, ErrNotAcceptableMediaType
}

// MarshalResponse selects appropriate resource marshaler based on [HTTPHeaderAccept] request header value and marshals response object.
func MarshalResponse(ctx *gin.Context, code int, responseValue any) {
	marshalResponse := ctx.MustGet(responseMarshalKey).(responseHandler)
	marshalResponse(code, responseValue)
}

// Ok writes HTTP/OK 200 response and marshals response object with [MarshalResponse] function
func Ok(ctx *gin.Context, resource any) {
	MarshalResponse(ctx, http.StatusOK, resource)
}

// MaybeGotOne checks usual API returned value if requested object exists, if there was an error during the request and
// if all good response [Ok] with requested resource marshaled using [MarshalResponse] function.
func MaybeGotOne(ctx *gin.Context, resource any, exists bool, err error) {
	if err != nil {
		AbortWithError(ctx, http.StatusBadRequest, err)
		return
	}
	if !exists {
		AbortWithError(ctx, http.StatusNotFound, ErrResourceNotFound)
		return
	}

	Ok(ctx, resource)
}

// Found is a shortcut to produce 200/Ok response for paginated data using [NewPaginatedResponse] to wrap items into Pagination frame.
func Found[T any](ctx *gin.Context, results []T, total int64, options ...HResponseOption) {
	searchParams := RequireSearchQueryParams(ctx)

	if ctx.Request.URL != nil {
		options = append(options, WithLink("self", manifest.HLink{Reference: ctx.Request.URL.String()}))
	}

	// If not the first page: give link to previous
	if searchParams.Page != 0 {
		relURL := *ctx.Request.URL

		query := relURL.Query()
		query.Set("page", fmt.Sprint(searchParams.Page-1))
		relURL.RawQuery = query.Encode()

		options = append(options, WithLink("prev", manifest.HLink{Reference: relURL.String()}))
	}

	// If not the last page:
	if len(results) > 0 && uint(len(results)) == searchParams.PageSize {
		relURL := *ctx.Request.URL

		query := relURL.Query()
		query.Set("page", fmt.Sprint(searchParams.Page+1))
		relURL.RawQuery = query.Encode()

		options = append(options, WithLink("next", manifest.HLink{Reference: relURL.String()}))
	}

	MarshalResponse(ctx, http.StatusOK, NewPaginatedResponse(results, total, searchParams.Pagination, options...))
}

// FoundOrNot checks error value and response with error or using [Found] function if no error.
func FoundOrNot[T any](ctx *gin.Context, err error, results []T, total int64, options ...HResponseOption) {
	if err != nil {
		AbortWithError(ctx, http.StatusBadRequest, err)
		return
	}

	Found[T](ctx, results, total, options...)
}

// ReplyResourceCreated is a shortcut to handle 201/Created response.
// It sets status code to [http.StatusCreated] and adds proper `Location` header to response headers.
func ReplyResourceCreated(ctx *gin.Context, id any, resource any) {
	ctx.Header(HTTPHeaderLocation, fmt.Sprintf("%v/%v", ctx.Request.URL.Path, id))

	MarshalResponse(ctx, http.StatusCreated, resource)
}

// AbortWithError terminates response-handling chain with an error, and returns provided HTTP error response to the client
func AbortWithError(ctx *gin.Context, code int, errValue error) {
	if errValue == nil {
		ctx.AbortWithStatus(code)
	} else if apiError, ok := errValue.(*ErrorResponse); ok {
		ctx.AbortWithStatusJSON(apiError.Code, apiError)
	} else {
		ctx.AbortWithStatusJSON(code, NewErrorResponse(code, errValue))
	}
}

// ContentTypeAPI returns middleware to support response marshaler selection based on [HTTPHeaderAccept] value.
// Used in conjunction with [MarshalResponse] and [ReplyResourceCreated]
func ContentTypeAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// select response encoder base of accept-type:
		if marshalResponse, err := replyWithAcceptedType(ctx); err != nil {
			ctx.AbortWithStatusJSON(http.StatusNotAcceptable, err)
			return
		} else {
			ctx.Set(responseMarshalKey, marshalResponse)
		}

		ctx.Next()
	}
}

// AcceptContentTypeAPI returns middleware to limit content-type [HTTPHeaderAccept] values accepted by the server.
func AcceptContentTypeAPI(accept ...string) gin.HandlerFunc {
	acceptable := manifest.NewStringSet(accept...)
	return func(ctx *gin.Context) {
		if !acceptable.Has(ctx.ContentType()) {
			ctx.AbortWithStatusJSON(http.StatusUnsupportedMediaType, ErrUnsupportedMediaType)
			return
		}

		ctx.Next()
	}
}

// SearchableAPI return middleware to support for [SearchQuery] parameter.
// See [RequireSearchQuery] usage on how to obtain [SearchQuery] value in the request handler
func SearchableAPI(defaultPaginationLimit uint) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var searchParams SearchParams
		if err := ctx.ShouldBindQuery(&searchParams); err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest, NewErrorResponse(http.StatusBadRequest, fmt.Errorf("bad search query: %w", err)))
		}

		if searchQuery, err := searchParams.BuildQuery(defaultPaginationLimit); err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest, NewErrorResponse(http.StatusBadRequest, fmt.Errorf("bad search query: %w", err)))
			return
		} else {
			searchParams.Pagination = searchParams.Pagination.ClampLimit(defaultPaginationLimit)

			ctx.Set(searchQueryParamsKey, searchParams)
			ctx.Set(searchQueryKey, searchQuery)
		}

		ctx.Next()
	}
}

// RequireSearchQueryParams returns [SearchParams] from the call context previously set by [SearchableAPI] middleware in the call chain.
// Note, the function should only be called from a handler that follows after [SearchableAPI] middleware in the filter chain.
func RequireSearchQueryParams(ctx *gin.Context) SearchParams {
	return ctx.MustGet(searchQueryParamsKey).(SearchParams)
}

// RequireSearchQuery returns [manifest.SearchQuery] from the call context previously set by [SearchableAPI] middleware in the call chain.
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
		if token, err := extractAuthBearer(ctx); err != nil {
			AbortWithError(ctx, http.StatusUnauthorized, err)
			return
		} else {
			ctx.Set(authBearerKey, token)
		}

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
