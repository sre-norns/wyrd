package bark

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const (
	// HTTPHeaderETag carries a resource's version on responses.
	HTTPHeaderETag = "ETag"
	// HTTPHeaderIfMatch carries the version a mutation was based on.
	HTTPHeaderIfMatch = "If-Match"

	ifMatchKey = "ifMatchVersion"
)

var (
	// ErrPreconditionRequired is returned for a mutation without If-Match.
	ErrPreconditionRequired error = manifest.NewStatusError(http.StatusPreconditionRequired, "precondition-required",
		"this request must say which version it changes, with If-Match")

	// ErrPreconditionFailed is returned when If-Match names a version other
	// than the current one: the change was based on stale state.
	ErrPreconditionFailed error = manifest.NewStatusError(http.StatusPreconditionFailed, "stale-version",
		"the resource has changed since the version this request was based on")

	// ErrMalformedIfMatch is returned for an If-Match that is not one version.
	ErrMalformedIfMatch error = manifest.NewStatusError(http.StatusBadRequest, "invalid-if-match",
		`If-Match must be a single version, as in "3", or *`)
)

// ETag formats a version as a strong entity tag.
func ETag(version manifest.Version) string {
	return `"` + version.String() + `"`
}

// SetETag sets the ETag response header to version (ADR 0001 §5).
func SetETag(ctx *gin.Context, version manifest.Version) {
	ctx.Header(HTTPHeaderETag, ETag(version))
}

// ParseIfMatch reads an If-Match value. any is true for `*`, which asks for no
// version check. A weak tag never matches under If-Match's strong comparison,
// so it is reported as a failed precondition rather than as malformed.
func ParseIfMatch(value string) (version manifest.Version, any bool, err error) {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return 0, false, ErrPreconditionRequired
	case value == "*":
		return 0, true, nil
	case strings.HasPrefix(value, "W/"):
		return 0, false, ErrPreconditionFailed
	case strings.Contains(value, ","):
		return 0, false, ErrMalformedIfMatch
	}

	unquoted, err := strconv.Unquote(value)
	if err != nil || !strings.HasPrefix(value, `"`) {
		return 0, false, ErrMalformedIfMatch
	}
	parsed, err := strconv.ParseUint(unquoted, 10, 64)
	if err != nil || parsed == 0 {
		return 0, false, ErrMalformedIfMatch
	}

	return manifest.Version(parsed), false, nil
}

// RequireIfMatch returns middleware requiring If-Match on PUT, PATCH and DELETE
// (ADR 0001 §5): 428 when it is missing, 400 when malformed. Other methods pass
// through. A handler reads the version with [IfMatchVersion] and passes it to
// the store's version guard; a stale one is reported as [ErrPreconditionFailed].
func RequireIfMatch() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		switch ctx.Request.Method {
		case http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			ctx.Next()
			return
		}

		version, any, err := ParseIfMatch(ctx.GetHeader(HTTPHeaderIfMatch))
		if err != nil {
			AbortWithProblem(ctx, http.StatusBadRequest, err)
			return
		}
		if !any {
			ctx.Set(ifMatchKey, version)
		}

		ctx.Next()
	}
}

// IfMatchVersion returns the version the request's If-Match named. ok is false
// when it named none (`*`, or no [RequireIfMatch] in the chain), in which case
// the change is unconditional.
func IfMatchVersion(ctx *gin.Context) (version manifest.Version, ok bool) {
	value, exists := ctx.Get(ifMatchKey)
	if !exists {
		return 0, false
	}
	version, ok = value.(manifest.Version)
	return version, ok
}

// versionOf returns the version of a resource a handler is about to send, if it
// has one.
func versionOf(resource any) (manifest.Version, bool) {
	switch r := resource.(type) {
	case manifest.ResourceManifest:
		return r.Metadata.Version, r.Metadata.Version != 0
	case *manifest.ResourceManifest:
		if r == nil {
			return 0, false
		}
		return r.Metadata.Version, r.Metadata.Version != 0
	case interface {
		GetVersionedID() manifest.VersionedResourceID
	}:
		v := r.GetVersionedID().Version
		return v, v != 0
	}
	return 0, false
}
