package manifest

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
)

// Selector is an interface for objects that can apply rules to match [Labels]
type Selector interface {
	// Matches returns true if the selector matches given label set.
	Matches(labels Labels) bool

	// Empty returns true if the selector is empty and thus will much everything.
	Empty() bool

	// Requirements returns collection of Requirements to expose more selection information.
	// FIXME: Leaking implementation details
	Requirements() (requirements labels.Requirements, selectable bool)
}

// k8Selector is implementation of a [Selector] using k8s api machinery types
type k8Selector struct {
	impl labels.Selector
}

func (s *k8Selector) Matches(labels Labels) bool {
	return s.impl.Matches(labels)
}

func (s *k8Selector) Empty() bool {
	return s.impl.Empty()
}

func (s *k8Selector) Requirements() (requirements labels.Requirements, selectable bool) {
	return s.impl.Requirements()
}

// ParseSelector parses a string that maybe represents a label based selector.
func ParseSelector(selector string) (Selector, error) {
	s, err := labels.Parse(selector)
	if err != nil {
		return nil, fmt.Errorf("error parsing labels selector: %w", err)
	}

	return &k8Selector{
		impl: s,
	}, nil
}

// SearchQuery represent query object accepted by APIs that implement pagination and label based object selection.
type SearchQuery struct {
	Selector Selector

	Offset uint `uri:"offset" form:"offset" json:"offset,omitempty" yaml:"offset,omitempty" xml:"offset"`
	Limit  uint `uri:"limit" form:"limit" json:"limit,omitempty" yaml:"limit,omitempty" xml:"limit"`
}
