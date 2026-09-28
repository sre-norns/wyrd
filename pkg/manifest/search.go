package manifest

import "time"

// SearchQuery represent query object accepted by APIs that implement pagination and label based object selection.
type SearchQuery struct {
	// Selector represents label-based filter to narrow down results.
	Selector Selector

	// Fields is a filter over server-known attributes rather than labels, in the
	// same grammar: `metadata.project=p1,status.phase in (running)`. Keeping it
	// apart from Selector means a user label named `status` is still selectable.
	Fields Selector

	// Cursor continues a listing where a previous page ended. It is opaque: a
	// value a store returned, never one a client builds.
	Cursor string `uri:"cursor" form:"cursor" json:"cursor,omitempty" yaml:"cursor,omitempty" xml:"cursor"`

	// Name is a fuzzy matched name of the resource to search for.
	Name string `uri:"name" form:"name" json:"name,omitempty" yaml:"name,omitempty" xml:"name"`
	// FromTime represents start of a time-range when searching for resources with time aspect.
	FromTime time.Time `uri:"from" form:"from" json:"from,omitempty" yaml:"from,omitempty" xml:"from"`
	// TillTime represents end of a time-range when searching for resources with time aspect.
	TillTime time.Time `uri:"till" form:"till" json:"till,omitempty" yaml:"till,omitempty" xml:"till"`

	// Offset is a number of items to skip when paginating a list of results
	Offset uint `uri:"offset" form:"offset" json:"offset,omitempty" yaml:"offset,omitempty" xml:"offset"`
	// Limit is the maximum number of results that a client can accept in return of the query.
	Limit uint `uri:"limit" form:"limit" json:"limit,omitempty" yaml:"limit,omitempty" xml:"limit"`
}

// Empty returns true is the Selector has Zero value, and thus impose no filter.
func (s SearchQuery) Empty() bool {
	return s.Limit == 0 && s.Offset == 0 &&
		s.FromTime.IsZero() && s.TillTime.IsZero() &&
		s.Name == "" && s.Cursor == "" &&
		(s.Selector == nil || s.Selector.Empty()) &&
		(s.Fields == nil || s.Fields.Empty())
}

// Page describes one page of a listing, as a store returns it and an API
// reports it.
type Page struct {
	// Limit is the page size used, after the default and the cap were applied.
	Limit uint `json:"limit" yaml:"limit"`

	// Next continues the listing after this page, as [SearchQuery.Cursor].
	// Empty on the last page: it is set only when a further row is known to
	// exist, never inferred from the page being full.
	Next string `json:"next,omitempty" yaml:"next,omitempty"`

	// Total is the number of matching rows, or nil when it was not counted. It
	// may be counted in a separate statement from the page, so under concurrent
	// writes it is advisory: never derive page links from it.
	Total *int64 `json:"total,omitempty" yaml:"total,omitempty"`
}
