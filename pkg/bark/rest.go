package bark

import (
	"fmt"
	"runtime/debug"
	"time"

	"github.com/ijt/go-anytime"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Common domain-agnostic types used to create rich REST APIs
type (

	// Pagination is a set of common pagination query params
	Pagination struct {
		Page     uint `uri:"page" form:"page" json:"page,omitempty" yaml:"page,omitempty" xml:"page"`
		PageSize uint `uri:"pageSize" form:"pageSize" json:"pageSize,omitempty" yaml:"pageSize,omitempty" xml:"pageSize"`
	}

	Timerange struct {
		// FromTime represents start of a time-range when searching for resources with time aspect.
		FromTime string `uri:"from" form:"from" json:"from,omitempty" yaml:"from,omitempty" xml:"from"`
		// TillTime represents end of a time-range when searching for resources with time aspect.
		TillTime string `uri:"till" form:"till" json:"till,omitempty" yaml:"till,omitempty" xml:"till"`
	}

	// Window is how a listing asks for a page (ADR 0001 §8): up to Limit rows,
	// continuing from Cursor. Offset is accepted for clients that page by
	// position; it is not combined with Cursor.
	//
	// OffsetParam and LimitParam are pointers so that an absent parameter can
	// be told from zero, and so from the older page/pageSize pair they replace.
	// They are not called Offset and Limit because [Pagination] already has
	// methods of those names, and SearchParams embeds both.
	Window struct {
		OffsetParam *uint  `uri:"offset" form:"offset" json:"offset,omitempty" yaml:"offset,omitempty" xml:"offset"`
		LimitParam  *uint  `uri:"limit" form:"limit" json:"limit,omitempty" yaml:"limit,omitempty" xml:"limit"`
		Cursor string `uri:"cursor" form:"cursor" json:"cursor,omitempty" yaml:"cursor,omitempty" xml:"cursor"`
	}

	// SearchParams represents grouping of query parameters commonly used by REST endpoint supporting search
	SearchParams struct {
		// Pagination is the older page/pageSize pair, kept as an alias for
		// Window's offset/limit.
		Pagination `uri:",inline" form:",inline" json:",inline" yaml:",inline"`
		Window     `uri:",inline" form:",inline" json:",inline" yaml:",inline"`
		Timerange  `uri:",inline" form:",inline" json:",inline" yaml:",inline"`

		// Name is a fuzzy matched name of the resource to search for.
		Name string `uri:"name" form:"name" json:"name,omitempty" yaml:"name,omitempty" xml:"name"`

		// Filter label-based filter to narrow down results.
		Filter string `uri:"labels" form:"labels" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels"`

		// Fields is a selector over server-known attributes, in the label
		// selector grammar. See [manifest.SearchQuery.Fields].
		Fields string `uri:"fields" form:"fields" json:"fields,omitempty" yaml:"fields,omitempty" xml:"fields"`
	}

	// PageLimits bounds the page size a listing may ask for.
	PageLimits struct {
		// Default is the page size of a request that asks for none.
		Default uint
		// Max is the largest page returned, whatever is asked for.
		Max uint
	}
)

// DefaultPageLimits are the portfolio's list bounds (ADR 0001 §8).
var DefaultPageLimits = PageLimits{Default: 100, Max: 1024}

// API Response types
type (

	// ListResponse is a page of a listing (ADR 0001 §8). Items is always an
	// array, empty rather than null on an empty page. Next is absent on the last
	// page, and Total is absent when it was not counted.
	ListResponse[T any] struct {
		Items []T    `form:"items" json:"items" yaml:"items" xml:"items"`
		Limit uint   `form:"limit" json:"limit" yaml:"limit" xml:"limit"`
		Next  string `form:"next" json:"next,omitempty" yaml:"next,omitempty" xml:"next"`
		Total *int64 `form:"total" json:"total,omitempty" yaml:"total,omitempty" xml:"total"`

		manifest.HResponse `form:",inline" json:",inline" yaml:",inline"`
	}

	// PaginatedResponse represents common frame used to produce response that returns a collection of results
	PaginatedResponse[T any] struct {
		Total int64 `form:"total" json:"total,omitempty" yaml:"total,omitempty" xml:"total"`
		Count int   `form:"count" json:"count,omitempty" yaml:"count,omitempty" xml:"count"`
		Data  []T   `form:"data" json:"data,omitempty" yaml:"data,omitempty" xml:"data"`

		manifest.HResponse `form:",inline" json:",inline" yaml:",inline"`
		Pagination         `form:",inline" json:",inline" yaml:",inline"`
	}

	// ErrorResponse represents a single error response with human readable reason and a code.
	ErrorResponse struct {
		// Error code represents error ID from a relevant domain
		Code int

		// Human readable representation of the error, suitable for display
		Message string

		manifest.HResponse `form:",inline" json:",inline" yaml:",inline"`
	}

	// StatusResponse represents ready state / healthcheck response
	StatusResponse struct {
		Ready bool `form:"ready" json:"ready,omitempty" yaml:"ready,omitempty" xml:"ready"`
	}

	// VersionResponse is a standard response object for /version request to inspect server running version.
	VersionResponse struct {
		Version   string `form:"version" json:"version,omitempty" yaml:"version,omitempty" xml:"version"`
		GoVersion string `form:"goVersion" json:"goVersion,omitempty" yaml:"goVersion,omitempty" xml:"goVersion"`
	}
)

// HResponseOption defines a type of 'optional' function that modifies HResponse properties when a new HResponse is constructed
type HResponseOption func(r *manifest.HResponse)

// WithLink returns an [HResponseOption] option that adds a HATEOAS link to a response object
func WithLink(role string, link manifest.HLink) HResponseOption {
	return func(r *manifest.HResponse) {
		if r == nil {
			return
		}

		if r.Links == nil {
			r.Links = make(map[string]manifest.HLink)
		}

		r.Links[role] = link
	}
}

// NewErrorResponse return new [ErrorResponse] object built from an object implementing [error] interface.
// The constructor returns nil if err argument is nil and no other options passed.
func NewErrorResponse(statusCode int, err error, options ...HResponseOption) (result *ErrorResponse) {
	if err == nil && len(options) == 0 {
		return
	}

	message := ""
	if err != nil {
		message = err.Error()
	}

	result = &ErrorResponse{
		Code:    statusCode,
		Message: message,
	}

	for _, o := range options {
		o(&result.HResponse)
	}

	return
}

// Error returns string representation of the error to implement error interface for [ErrorResponse] type.
func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("%v %s", e.Code, e.Message)
}

// Offset returns a 0-based index if a pagination was continues.
func (p Pagination) Offset() uint {
	return p.Page * p.PageSize
}

// Limit returns maximum number of items that a query should return.
// Default value of 0 means that a client haven't specified a limit and the server will use default value.
func (p Pagination) Limit() uint {
	return p.PageSize
}

// ClampLimit returns new pagination object that has its [Pagination.Limit] clamped to a value in between [0, maxLimit] range.
// If current value of of [Pagination.Limit] is within the [0, maxLimit] range then the value is unchanged,
// if the value of [Pagination.Limit] is outside of [0, maxLimit] range, maxLimit is used.
func (p Pagination) ClampLimit(maxLimit uint) Pagination {
	result := Pagination{
		Page:     p.Page,
		PageSize: p.PageSize,
	}

	if result.PageSize > maxLimit || result.PageSize == 0 {
		result.PageSize = maxLimit
	}

	return result
}

// BuildQuery returns a [manifest.SearchQuery] query object if the [SearchParams] can be converted to it.
//
// defaultLimit is both the page size of a request asking for none and the cap:
// the behaviour of releases before [PageLimits]. Use [SearchParams.BuildQueryWithLimits]
// to set them apart.
func (s SearchParams) BuildQuery(defaultLimit uint) (manifest.SearchQuery, error) {
	return s.BuildQueryWithLimits(PageLimits{Default: defaultLimit, Max: defaultLimit})
}

// ErrConflictingPagination is returned when a request pages in two ways at once.
var ErrConflictingPagination = fmt.Errorf("conflicting pagination parameters")

// BuildQueryWithLimits returns the [manifest.SearchQuery] the parameters ask
// for, with the page size bounded by limits.
func (s SearchParams) BuildQueryWithLimits(limits PageLimits) (manifest.SearchQuery, error) {
	selector, err := manifest.ParseSelector(s.Filter)
	if err != nil {
		return manifest.SearchQuery{}, err
	}

	var fields manifest.Selector
	if s.Fields != "" {
		if fields, err = manifest.ParseSelector(s.Fields); err != nil {
			return manifest.SearchQuery{}, fmt.Errorf("bad field selector: %w", err)
		}
	}

	offset, limit, err := s.window(limits)
	if err != nil {
		return manifest.SearchQuery{}, err
	}

	refTime := time.Now()

	var from time.Time
	if s.FromTime != "" {
		t, err := anytime.Parse(s.FromTime, refTime)
		if err != nil {
			return manifest.SearchQuery{}, fmt.Errorf("failed to parse 'from' date: %w", err)
		}
		from = t
	}
	var till time.Time
	if s.TillTime != "" {
		t, err := anytime.Parse(s.TillTime, refTime)
		if err != nil {
			return manifest.SearchQuery{}, fmt.Errorf("failed to parse 'till' date: %w", err)
		}
		till = t
	}

	if s.FromTime != "" && s.TillTime != "" {
		if from.After(till) {
			return manifest.SearchQuery{}, fmt.Errorf("'from' date (%v) is after 'till' (%v)", from, till)
		}
	}

	return manifest.SearchQuery{
		Selector: selector,
		Fields:   fields,
		Name:     s.Name,

		FromTime: from,
		TillTime: till,

		Cursor: s.Cursor,
		Offset: offset,
		Limit:  limit,
	}, nil
}

// window resolves the requested offset and limit. offset and limit take
// precedence over the older page and pageSize, each on its own, rather than
// being refused alongside them: consumers set a default pageSize before building
// the query, and a client's explicit limit must still win over it.
func (s SearchParams) window(limits PageLimits) (offset, limit uint, err error) {
	if s.Cursor != "" && (s.Page != 0 || (s.OffsetParam != nil && *s.OffsetParam != 0)) {
		return 0, 0, fmt.Errorf("%w: a cursor already says where the page starts", ErrConflictingPagination)
	}

	limit = s.PageSize
	if s.LimitParam != nil {
		limit = *s.LimitParam
	}
	if limit == 0 {
		limit = limits.Default
	}
	if limits.Max > 0 && limit > limits.Max {
		limit = limits.Max
	}

	offset = s.Page * limit
	if s.OffsetParam != nil {
		offset = *s.OffsetParam
	}

	return offset, limit, nil
}

// NewListResponse returns items as a page described by page. A nil items is
// sent as an empty array.
func NewListResponse[T any](items []T, page manifest.Page, options ...HResponseOption) ListResponse[T] {
	if items == nil {
		items = []T{}
	}

	result := ListResponse[T]{
		Items: items,
		Limit: page.Limit,
		Next:  page.Next,
		Total: page.Total,
	}
	for _, o := range options {
		o(&result.HResponse)
	}

	return result
}

// NewPaginatedResponse creates a new paginated response with options to adjust HATEOAS response params
func NewPaginatedResponse[T any](items []T, total int64, pInfo Pagination, options ...HResponseOption) PaginatedResponse[T] {
	result := PaginatedResponse[T]{
		Data:       items,
		Total:      total,
		Count:      len(items),
		Pagination: pInfo,
	}

	for _, o := range options {
		o(&result.HResponse)
	}

	return result
}

func NewVersionResponse() VersionResponse {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return VersionResponse{
			Version: "unknown",
		}
	}

	return VersionResponse{
		Version:   bi.Main.Version,
		GoVersion: bi.GoVersion,
	}
}
