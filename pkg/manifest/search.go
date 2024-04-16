package manifest

import "time"

// SearchQuery represent query object accepted by APIs that implement pagination and label based object selection.
type SearchQuery struct {
	// Selector represents label-based filter to narrow down results.
	Selector Selector

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
