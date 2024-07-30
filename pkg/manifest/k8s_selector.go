package manifest

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
)

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

func (s *k8Selector) Requirements() (requirements Requirements, selectable bool) {
	rules, ok := s.impl.Requirements()
	if !ok {
		return nil, ok
	}

	results := make(Requirements, 0, len(rules))
	for _, rule := range rules {
		req, err := NewRequirement(rule.Key(), Operator(rule.Operator()), rule.Values().List())
		if err != nil {
			return nil, false
		}

		results = append(results, req)
	}

	return results, true
}

func (s *k8Selector) String() string {
	if s == nil || s.impl == nil {
		return "<nil>"
	}

	return s.impl.String()
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
