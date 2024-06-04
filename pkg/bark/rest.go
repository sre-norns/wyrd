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

	// SearchParams represents grouping of query parameters commonly used by REST endpoint supporting search
	SearchParams struct {
		Pagination `uri:",inline" form:",inline" json:",inline" yaml:",inline"`
		Timerange  `uri:",inline" form:",inline" json:",inline" yaml:",inline"`

		// Name is a fuzzy matched name of the resource to search for.
		Name string `uri:"name" form:"name" json:"name,omitempty" yaml:"name,omitempty" xml:"name"`

		// Filter label-based filter to narrow down results.
		Filter string `uri:"labels" form:"labels" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels"`
	}
)

// API Response types
type (

	// PaginatedResponse represents common frame used to produce response that returns a collection of results
	PaginatedResponse[T any] struct {
		Total int `form:"total" json:"total,omitempty" yaml:"total,omitempty" xml:"total"`
		Count int `form:"count" json:"count,omitempty" yaml:"count,omitempty" xml:"count"`
		Data  []T `form:"data" json:"data,omitempty" yaml:"data,omitempty" xml:"data"`

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
func (s SearchParams) BuildQuery(defaultLimit uint) (manifest.SearchQuery, error) {
	selector, err := manifest.ParseSelector(s.Filter)
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

	pagination := s.Pagination.ClampLimit(defaultLimit)
	return manifest.SearchQuery{
		Selector: selector,
		Name:     s.Name,

		FromTime: from,
		TillTime: till,

		Offset: pagination.Offset(),
		Limit:  pagination.Limit(),
	}, nil
}

// NewPaginatedResponse creates a new paginated response with options to adjust HATEOAS response params
func NewPaginatedResponse[T any](items []T, total int, pInfo Pagination, options ...HResponseOption) PaginatedResponse[T] {
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
