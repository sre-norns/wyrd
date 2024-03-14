package bark

import (
	"fmt"
)

// Common domain-agnostic types used to create rich REST APIs
type (

	// Pagination is a set of common pagination query params
	Pagination struct {
		Offset uint `uri:"offset" form:"offset" json:"offset,omitempty" yaml:"offset,omitempty" xml:"offset"`
		Limit  uint `uri:"limit" form:"limit" json:"limit,omitempty" yaml:"limit,omitempty" xml:"limit"`
	}

	// SearchQuery provides grouping of of query parameters commonly used by REST endpoint that perform search
	SearchQuery struct {
		Pagination `uri:",inline" form:",inline"`
		Filter     string `uri:"labels" form:"labels" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels"`
	}
)

// API Response types
type (
	// HLink is a struct to hold semantic web links, representing action that can be performed on response item
	HLink struct {
		Reference    string `form:"ref" json:"ref" yaml:"ref" xml:"ref"`
		Relationship string `form:"rel" json:"rel" yaml:"rel" xml:"rel"`
	}

	// HResponse is a response object, produced by a server that has semantic references
	HResponse struct {
		Links map[string]HLink `form:"_links" json:"_links,omitempty" yaml:"_links,omitempty" xml:"_links"`
	}

	// PaginatedResponse represents common frame used to produce response that returns a collection of results
	PaginatedResponse[T any] struct {
		Count int `form:"count" json:"count,omitempty" yaml:"count,omitempty" xml:"count"`
		Data  []T `form:"data" json:"data,omitempty" yaml:"data,omitempty" xml:"data"`

		HResponse  `form:",inline" json:",inline" yaml:",inline"`
		Pagination `form:",inline" json:",inline" yaml:",inline"`
	}

	// ErrorResponse represents a single error response with human readable reason and a code.
	ErrorResponse struct {
		// Error code represents error ID from a relevant domain
		Code int

		// Human readable representation of the error, suitable for display
		Message string

		HResponse `form:",inline" json:",inline" yaml:",inline"`
	}
)

type HResponseOption func(r *HResponse)

// WithLink returns an [HResponseOption] option that adds a HEATOS link to a response object
func WithLink(role string, link HLink) HResponseOption {
	return func(r *HResponse) {
		if r == nil {
			return
		}

		if r.Links == nil {
			r.Links = make(map[string]HLink)
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

// ClampLimit returns new pagination object that has its [Pagination.Limit] clamped to a value in between [0, maxLimit] range.
// If current value of of [Pagination.Limit] is within the [0, maxLimit] range then the value is unchanged,
// if the value of [Pagination.Limit] is outside of [0, maxLimit] range, maxLimit is used.
func (p Pagination) ClampLimit(maxLimit uint) Pagination {
	result := Pagination{
		Offset: p.Offset,
		Limit:  p.Limit,
	}

	if result.Limit > maxLimit || result.Limit == 0 {
		result.Limit = maxLimit
	}

	return result
}
