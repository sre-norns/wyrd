package manifest

import (
	"errors"
	"fmt"
)

type Operator string

const (
	DoesNotExist Operator = "!"
	Equals       Operator = "="
	DoubleEquals Operator = "=="
	In           Operator = "in"
	NotEquals    Operator = "!="
	NotIn        Operator = "notin"
	Exists       Operator = "exists"
	GreaterThan  Operator = "gt"
	LessThan     Operator = "lt"
)

var ErrInvalidOperator = errors.New("invalid operator")

func IsValidOperator(op string) bool {
	switch Operator(op) {
	case DoesNotExist, Equals, DoubleEquals, In, NotEquals, NotIn, Exists, GreaterThan, LessThan:
		return true
	}
	return false
}

// Requirement represents a rule/requirement that a selector is using when evaluating matches
type Requirement struct {
	// Key is the name of the key to select
	key string
	// Op is a math operation to perform. See [LabelSelectorOperator] doc for more info.
	operator Operator
	// Values is an optional list of value to apply [SelectorRule.Op] to. For Operator like [Exist] the list must be empty.
	values StringSet
}

// Requirements Represents a collection of requirements.
type Requirements []Requirement

func NewRequirement(key, op string, values []string) (Requirement, error) {
	// TODO: Validate key
	if !IsValidOperator(op) {
		return Requirement{}, fmt.Errorf("%w: %v", ErrInvalidOperator, op)
	}

	valSet := make(StringSet)
	for _, v := range values {
		valSet[v] = struct{}{}
	}

	return Requirement{
		key:      key,
		operator: Operator(op),
		values:   valSet,
	}, nil
}

func (r *Requirement) Key() string {
	return r.key
}

func (r *Requirement) Operator() Operator {
	return r.operator
}

func (r *Requirement) Values() StringSet {
	return r.values
}

// Selector is an interface for objects that can apply rules to match [Labels]
type Selector interface {
	// Matches returns true if the selector matches given label set.
	Matches(labels Labels) bool

	// Empty returns true if the selector is empty and thus will much everything.
	Empty() bool

	// Requirements returns collection of Requirements to expose more selection information.
	// FIXME: Leaking implementation details
	Requirements() (requirements Requirements, selectable bool)
}

// SearchQuery represent query object accepted by APIs that implement pagination and label based object selection.
type SearchQuery struct {
	Selector Selector

	Offset uint `uri:"offset" form:"offset" json:"offset,omitempty" yaml:"offset,omitempty" xml:"offset"`
	Limit  uint `uri:"limit" form:"limit" json:"limit,omitempty" yaml:"limit,omitempty" xml:"limit"`
}
