package bark

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ResourceRequest represents information to identify a single resource being referred in the path / query
type (
	ResourceRequest struct {
		ID string `uri:"id" form:"id" binding:"required"`
	}

	// VersionQuery is a set of query params for the versioned resource,
	// such as specific version number of the resource in questions
	VersionQuery struct {
		Version manifest.Version `uri:"version" form:"version" binding:"required"`
	}

	// CreatedResponse represents information about newly created resource that is returned in response to 'Create' call.
	CreatedResponse struct {
		// Gives us kind info
		manifest.TypeMeta `json:",inline" yaml:",inline"`

		// Id and version information of the newly created resource
		manifest.VersionedResourceID `json:",inline" yaml:",inline"`

		// Semantic actions
		manifest.HResponse `form:",inline" json:",inline" yaml:",inline"`
	}
)

// ManifestAPI returns middleware that extract [manifest.ResourceManifest] from an incoming request body.
// [HTTPHeaderContentType] is used for content-type negotiation.
// Note, the call is terminated if incorrect [manifest.kind] is passed to the API.
// Refer to [RequireManifest] to get extract manifest form the call context.
func ManifestAPI(expectedKind manifest.Kind) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		manifest := manifest.ResourceManifest{
			TypeMeta: manifest.TypeMeta{
				Kind: expectedKind, // Assume correct kind in case of run-triggers with min info
			},
		}
		if err := ctx.ShouldBindWith(&manifest, bindingFor(ctx.Request.Method, ctx.ContentType())); err != nil {
			AbortWithError(ctx, http.StatusBadRequest, fmt.Errorf("failed to parse manifest body: %w", err))
			return
		}

		if manifest.Kind == "" && expectedKind != "" {
			manifest.Kind = expectedKind
		} else if manifest.Kind != expectedKind { // validate that API request is for correct manifest type:
			AbortWithError(ctx, http.StatusBadRequest, ErrWrongKind)
			return
		}

		ctx.Set(resourceManifestKey, manifest)
		ctx.Next()
	}
}

// RequireManifest returns [manifest.ResourceManifest] instance parsed out of request body by [ManifestAPI] middleware.
// Note: [ManifestAPI] middleware must be setup in the call chain before this call.
func RequireManifest(ctx *gin.Context) manifest.ResourceManifest {
	return ctx.MustGet(resourceManifestKey).(manifest.ResourceManifest)
}

// ResourceAPI return a middleware to add support for parsing of resource IDs from request path.
// See [RequireResourceID] about how to access [ResourceRequest] containing passed resource ID.
func ResourceAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var resourceRequest ResourceRequest
		if err := ctx.BindUri(&resourceRequest); err != nil {
			AbortWithError(ctx, http.StatusNotFound, err)
			return
		} else {
			ctx.Set(resourceIDKey, resourceRequest)
		}

		ctx.Next()
	}
}

// RequireResourceID return [manifest.ResourceID] previously extracted by [ResourceAPI] middleware, containing ID of the requested resource from the path.
// Note: must be used from a request handler that follows [ResourceAPI] middleware in the call chain.
func RequireResourceID(ctx *gin.Context) manifest.ResourceID {
	return manifest.ResourceID(ctx.MustGet(resourceIDKey).(ResourceRequest).ID)
}

// RequireResourceName return [manifest.ResourceName] previously extracted by [ResourceAPI] middleware, containing a Name of the requested resource from the path.
// Note: must be used from a request handler that follows [ResourceAPI] middleware in the call chain.
func RequireResourceName(ctx *gin.Context) manifest.ResourceName {
	return manifest.ResourceName(ctx.MustGet(resourceIDKey).(ResourceRequest).ID)
}

// VersionedResourceAPI returns middleware that reads Resource ID and Version query parameter
// in the request URL.
// See [RequireVersionedResource] and [RequireVersionedResourceQuery] for information
// on how to extract [VersionedResourceID] from the call context.
func VersionedResourceAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var versionInfo VersionQuery
		if err := ctx.ShouldBindQuery(&versionInfo); err != nil {
			AbortWithError(ctx, http.StatusBadRequest, err)
			return
		}

		if resourceID, ok := ctx.Get(resourceIDKey); ok {
			ctx.Set(versionedIDKey, manifest.NewVersionedID(manifest.ResourceID(resourceID.(ResourceRequest).ID), versionInfo.Version))
		}

		ctx.Set(versionInfoKey, versionInfo)
		ctx.Next()
	}
}

// RequireVersionedResource is a helper function to extract [manifest.VersionedResourceID] from the call context.
// Note, it must be called only from a handler that follows after [VersionedResourceAPI] middleware in the call-chain.
func RequireVersionedResource(ctx *gin.Context) manifest.VersionedResourceID {
	return ctx.MustGet(versionedIDKey).(manifest.VersionedResourceID)
}

// RequireVersionedResourceQuery is a helper function to extract [VersionQuery] from the call context.
// Note, it must be called only from a handler that follows after [VersionedResourceAPI] middleware in the call-chain.
func RequireVersionedResourceQuery(ctx *gin.Context) VersionQuery {
	return ctx.MustGet(versionInfoKey).(VersionQuery)
}

// MaybeManifest writes a manifest resource into response if there was not error and resource exists
func MaybeManifest(ctx *gin.Context, resource manifest.ResourceManifest, exists bool, err error, options ...HResponseOption) {
	// Apply options:
	if exists && err == nil {
		for _, o := range options {
			o(&resource.HResponse)
		}
	}

	MaybeGotOne(ctx, resource, exists, err)
}

// MaybeResourceCreated is a shortcut to handle 201/Created response.
// It sets status code to [http.StatusCreated] and adds proper `Location` header to response headers.
func MaybeResourceCreated(ctx *gin.Context, resource manifest.ResourceManifest, err error, options ...HResponseOption) {
	if err != nil {
		AbortWithError(ctx, http.StatusBadRequest, err)
		return
	}

	// Apply options:
	for _, o := range options {
		o(&resource.HResponse)
	}

	// Setup 'Location' header and write response
	ReplyResourceCreated(ctx, resource.Metadata.Name, resource)
}
