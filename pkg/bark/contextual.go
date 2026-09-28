package bark

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type contextualResponse[T any] struct {
	ctx *gin.Context

	options []HResponseOption
}

func Manifest(ctx *gin.Context) *contextualResponse[manifest.ResourceManifest] {
	return &contextualResponse[manifest.ResourceManifest]{
		ctx: ctx,
	}
}

func WithContext[T any](ctx *gin.Context) *contextualResponse[T] {
	return &contextualResponse[T]{
		ctx: ctx,
	}
}

func (c *contextualResponse[T]) WithOptions(options ...HResponseOption) *contextualResponse[T] {
	return &contextualResponse[T]{
		ctx:     c.ctx,
		options: append(c.options, options...),
	}
}

func (c *contextualResponse[T]) AbortWithError(code int, err error) {
	AbortWithError(c.ctx, http.StatusBadRequest, err)
}

func (c *contextualResponse[T]) CreatedOrUpdated(resource T, created bool, err error) {
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	if created {
		c.Created(resource, err)
	} else {
		Ok(c.ctx, resource)
	}
}

func (c *contextualResponse[T]) List(results []T, total int64, err error) {
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
	} else {
		c.listPage(results, total)
	}
}

func (c *contextualResponse[T]) Created(value T, err error) {
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
	} else {
		// Setup 'Location' header and write response
		// c.ctx.Header(HTTPHeaderLocation, fmt.Sprintf("%v/%v", c.ctx.Request.URL.Path, id))

		MarshalResponse(c.ctx, http.StatusCreated, value)
	}
}

func (c *contextualResponse[T]) Found(resource T, exist bool, err error) {
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
	} else if !exist {
		c.AbortWithError(http.StatusNotFound, ErrResourceNotFound)
	} else {
		if version, ok := versionOf(resource); ok {
			SetETag(c.ctx, version)
		}
		Ok(c.ctx, resource)
	}
}

func (c *contextualResponse[T]) Deleted(existed bool, err error) {
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
	} else if !existed {
		c.AbortWithError(http.StatusNotFound, ErrResourceNotFound)
	} else {
		c.ctx.Status(http.StatusNoContent)
	}
}

func (c *contextualResponse[T]) listPage(results []T, total int64) {
	var selfURL *url.URL
	if c.ctx.Request.URL != nil {
		selfURL = c.ctx.Request.URL
		c.options = append(c.options, WithLink("self", manifest.HLink{Reference: selfURL.String()}))
	}

	searchParams := RequireSearchQueryParams(c.ctx)
	// If not the first page: give link to previous
	if selfURL != nil && searchParams.Page > 0 {
		relURL := *selfURL

		query := relURL.Query()
		query.Set("page", fmt.Sprint(searchParams.Page-1))
		relURL.RawQuery = query.Encode()

		c.options = append(c.options, WithLink("prev", manifest.HLink{Reference: relURL.String()}))
	}

	// If not the last page:
	if selfURL != nil && len(results) > 0 && uint(len(results)) == searchParams.PageSize {
		relURL := *selfURL

		query := relURL.Query()
		query.Set("page", fmt.Sprint(searchParams.Page+1))
		relURL.RawQuery = query.Encode()

		c.options = append(c.options, WithLink("next", manifest.HLink{Reference: relURL.String()}))
	}

	MarshalResponse(c.ctx, http.StatusOK, NewPaginatedResponse(results, total, searchParams.Pagination, c.options...))
}

// Page responds with one page of a listing in the [ListResponse] shape. The
// next link is built from page.Next, never inferred from the page being full,
// and it carries the cursor in place of any offset or page parameters.
func (c *contextualResponse[T]) Page(results []T, page manifest.Page, err error) {
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	if self := c.ctx.Request.URL; self != nil {
		c.options = append(c.options, WithLink("self", manifest.HLink{Reference: self.String()}))

		if page.Next != "" {
			next := *self
			query := next.Query()
			for _, stale := range []string{"offset", "page", "cursor"} {
				query.Del(stale)
			}
			query.Set("cursor", page.Next)
			next.RawQuery = query.Encode()
			c.options = append(c.options, WithLink("next", manifest.HLink{Reference: next.String()}))
		}
	}

	MarshalResponse(c.ctx, http.StatusOK, NewListResponse(results, page, c.options...))
}
