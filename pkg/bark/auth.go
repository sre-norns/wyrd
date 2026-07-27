package bark

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	authBearerKey = "Bearer"
	bearerScheme  = "Bearer"

	// HTTPHeaderAuth is the standard header used to communicate authorization information.
	HTTPHeaderAuth = "Authorization"

	// HTTPHeaderWWWAuthenticate is the standard header used to challenge a client for authentication credentials.
	HTTPHeaderWWWAuthenticate = "WWW-Authenticate"
)

// BearerParseErrorKind identifies why an Authorization header could not be
// parsed as RFC 6750 Bearer credentials.
type BearerParseErrorKind uint8

const (
	// BearerParseHeaderMissing means no Authorization value was provided.
	BearerParseHeaderMissing BearerParseErrorKind = iota + 1
	// BearerParseSchemeInvalid means the credentials use a scheme other than Bearer.
	BearerParseSchemeInvalid
	// BearerParseTokenMissing means the Bearer scheme has no token.
	BearerParseTokenMissing
	// BearerParseCredentialsExtra means whitespace-delimited data follows the token.
	BearerParseCredentialsExtra
	// BearerParseTokenInvalid means the token does not match RFC 6750 b64token syntax.
	BearerParseTokenInvalid
)

var (
	// ErrInvalidAuthHeader indicates that an Authorization header is not valid
	// Bearer credentials. Use errors.As with BearerParseError when the specific
	// parse failure is needed.
	ErrInvalidAuthHeader = errors.New("invalid Authorization header")

	errBearerHeaderMissing    = &BearerParseError{kind: BearerParseHeaderMissing}
	errBearerSchemeInvalid    = &BearerParseError{kind: BearerParseSchemeInvalid}
	errBearerTokenMissing     = &BearerParseError{kind: BearerParseTokenMissing}
	errBearerCredentialsExtra = &BearerParseError{kind: BearerParseCredentialsExtra}
	errBearerTokenInvalid     = &BearerParseError{kind: BearerParseTokenInvalid}
)

// BearerParseError is a credential-free classified Bearer parsing error.
// It never retains or renders the input header or token.
type BearerParseError struct {
	kind BearerParseErrorKind
}

// Kind returns the classified Bearer parsing failure.
func (e *BearerParseError) Kind() BearerParseErrorKind {
	if e == nil {
		return 0
	}
	return e.kind
}

func (e *BearerParseError) Error() string {
	if e == nil {
		return ErrInvalidAuthHeader.Error()
	}

	switch e.kind {
	case BearerParseHeaderMissing:
		return "invalid Authorization header: missing header"
	case BearerParseSchemeInvalid:
		return "invalid Authorization header: invalid authentication scheme"
	case BearerParseTokenMissing:
		return "invalid Authorization header: missing bearer token"
	case BearerParseCredentialsExtra:
		return "invalid Authorization header: extra bearer credentials"
	case BearerParseTokenInvalid:
		return "invalid Authorization header: malformed bearer token"
	default:
		return ErrInvalidAuthHeader.Error()
	}
}

// Unwrap preserves compatibility with ErrInvalidAuthHeader.
func (e *BearerParseError) Unwrap() error {
	return ErrInvalidAuthHeader
}

// ParseBearer extracts a Bearer token from an Authorization header value.
//
// The accepted syntax is the RFC 6750 credential form: a case-insensitive
// "Bearer" scheme, one or more ASCII spaces, and one b64token. Horizontal tabs,
// leading/trailing whitespace, credential lists, and characters outside the
// b64token alphabet are rejected. ParseBearer validates syntax only; it does not
// authenticate the token.
func ParseBearer(authorization string) (string, error) {
	if authorization == "" {
		return "", errBearerHeaderMissing
	}

	separator := strings.IndexByte(authorization, ' ')
	if separator < 0 {
		if strings.EqualFold(authorization, bearerScheme) {
			return "", errBearerTokenMissing
		}
		if len(authorization) > len(bearerScheme) &&
			strings.EqualFold(authorization[:len(bearerScheme)], bearerScheme) {
			return "", errBearerTokenInvalid
		}
		return "", errBearerSchemeInvalid
	}
	if !strings.EqualFold(authorization[:separator], bearerScheme) {
		return "", errBearerSchemeInvalid
	}

	tokenStart := separator + 1
	for tokenStart < len(authorization) && authorization[tokenStart] == ' ' {
		tokenStart++
	}
	if tokenStart == len(authorization) {
		return "", errBearerTokenMissing
	}

	token := authorization[tokenStart:]
	if strings.ContainsAny(token, " \t") {
		return "", errBearerCredentialsExtra
	}
	if !isBearerToken(token) {
		return "", errBearerTokenInvalid
	}

	return token, nil
}

func isBearerToken(token string) bool {
	padding := false
	for i := range len(token) {
		switch {
		case token[i] == '=':
			if i == 0 {
				return false
			}
			padding = true
		case 'A' <= token[i] && token[i] <= 'Z',
			'a' <= token[i] && token[i] <= 'z',
			'0' <= token[i] && token[i] <= '9',
			token[i] == '-', token[i] == '.', token[i] == '_', token[i] == '~',
			token[i] == '+', token[i] == '/':
			if padding {
				return false
			}
		default:
			return false
		}
	}

	return len(token) > 0
}

// AuthBearerAPI returns middleware that extracts Bearer credentials from an
// incoming request. Parse failures are collapsed to one safe public 401
// response; token validity and authorization remain caller responsibilities.
func AuthBearerAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, err := ParseBearer(ctx.Request.Header.Get(HTTPHeaderAuth))
		if err != nil {
			ctx.Header(HTTPHeaderWWWAuthenticate, bearerScheme)
			AbortWithError(ctx, http.StatusUnauthorized, ErrResourceUnauthorized)
			return
		}

		ctx.Set(authBearerKey, token)
		ctx.Next()
	}
}

// RequireBearerToken returns the Bearer token extracted by AuthBearerAPI.
// It must only be called after that middleware in the request chain.
func RequireBearerToken(ctx *gin.Context) string {
	return ctx.MustGet(authBearerKey).(string)
}
