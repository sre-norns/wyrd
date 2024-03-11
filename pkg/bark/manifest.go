package bark

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ResourceRequest represents information to identify a single resource being referred in the path / query
type (
	ResourceRequest struct {
		ID manifest.ResourceID `uri:"id" form:"id" binding:"required"`
	}

	// VersionQuery is a set of query params for the versioned resource,
	// such as specific version number of the resource in questions
	VersionQuery struct {
		Version manifest.Version `uri:"version" form:"version" binding:"required"`
	}

	// CreatedResponse return information about newly created resource
	CreatedResponse struct {
		// Gives us kind info
		manifest.TypeMeta `json:",inline" yaml:",inline"`

		// Id and version information of the newly created resource
		manifest.VersionedResourceID `json:",inline" yaml:",inline"`

		// Semantic actions
		HResponse `form:",inline" json:",inline" yaml:",inline"`
	}
)

func ManifestAPI(kind manifest.Kind) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		manifest := manifest.ResourceManifest{
			TypeMeta: manifest.TypeMeta{
				Kind: kind, // Assume correct kind in case of run-triggers with min info
			},
		}
		if err := ctx.ShouldBindWith(&manifest, bindingFor(ctx.Request.Method, ctx.ContentType())); err != nil {
			AbortWithError(ctx, http.StatusBadRequest, err)
			return
		}

		if manifest.Kind == "" {
			manifest.Kind = kind
		} else if manifest.Kind != kind { // validate that API request is for correct manifest type:
			AbortWithError(ctx, http.StatusBadRequest, ErrWrongKind)
			return
		}

		ctx.Set(resourceManifestKey, manifest)
		ctx.Next()
	}
}

func RequireManifest(ctx *gin.Context) manifest.ResourceManifest {
	return ctx.MustGet(resourceManifestKey).(manifest.ResourceManifest)
}

// ResourceAPI is a filter/middleware to add support for resource ID requests
// Used in conjunction with `RequireResourceId`
func ResourceAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var resourceRequest ResourceRequest
		if err := ctx.BindUri(&resourceRequest); err != nil {
			AbortWithError(ctx, http.StatusNotFound, err)
			return
		}

		ctx.Set(resourceIDKey, resourceRequest)
		ctx.Next()
	}
}

// RequireResourceID is a shortcut function to get ID of the requested resource from the path.
// Note: must be used from a request handler that includes `ResourceAPI` in the call chain.
func RequireResourceID(ctx *gin.Context) ResourceRequest {
	return ctx.MustGet(resourceIDKey).(ResourceRequest)
}

// VersionedResourceAPI is a middleware to support Resource ID and Version query parameter
// in the request URL.
// See `RequireVersionedResource` and `RequireVersionedResourceQuery` for information
// on how to extract `VersionedResourceID` from the context
func VersionedResourceAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var versionInfo VersionQuery
		if err := ctx.ShouldBindQuery(&versionInfo); err != nil {
			AbortWithError(ctx, http.StatusBadRequest, err)
			return
		}

		if resourceID, ok := ctx.Get(resourceIDKey); ok {
			ctx.Set(versionedIDKey, manifest.NewVersionedID(resourceID.(ResourceRequest).ID, versionInfo.Version))
		}

		ctx.Set(versionInfoKey, versionInfo)
		ctx.Next()
	}
}

// RequireVersionedResource is a helper method to extract `VersionedResourceID` from the call context.
// It must be called after `VersionedResourceAPI()` middleware
func RequireVersionedResource(ctx *gin.Context) manifest.VersionedResourceID {
	return ctx.MustGet(versionedIDKey).(manifest.VersionedResourceID)
}

// RequireVersionedResourceQuery is a helper method to extract `VersionQuery` from the call context.
// It must be called after `VersionedResourceAPI()` middleware
func RequireVersionedResourceQuery(ctx *gin.Context) VersionQuery {
	return ctx.MustGet(versionInfoKey).(VersionQuery)
}
