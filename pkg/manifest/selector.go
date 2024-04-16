package manifest

import (
	"errors"
	"fmt"
	"strconv"
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

func (r *Requirement) hasValue(value string) bool {
	return r.values != nil && r.values.Has(value)
}

func (r *Requirement) Matches(labels Labels) bool {
	switch r.operator {
	case Exists:
		return labels.Has(r.key)
	case DoesNotExist:
		return !labels.Has(r.key)
	case In, Equals, DoubleEquals:
		return labels.Has(r.key) && r.hasValue(labels.Get(r.key))
	case NotIn, NotEquals:
		return !labels.Has(r.key) || !r.hasValue(labels.Get(r.key))
	case GreaterThan, LessThan:
		if !labels.Has(r.key) {
			return false
		}
		if len(r.values) != 1 {
			return false
		}

		lsValue, err := strconv.ParseInt(labels.Get(r.key), 10, 64)
		if err != nil {
			return false
		}

		var rValue int64
		for v := range r.values {
			rValue, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				return false
			}
		}

		return (r.operator == GreaterThan && lsValue > rValue) || (r.operator == LessThan && lsValue < rValue)
	default:
		return false
	}
}

// Selector is an interface for objects that can apply rules to match [Labels]
type Selector interface {
	// Matches returns true if the selector matches given label set.
	Matches(labels Labels) bool

	// Empty returns true if the selector is empty and thus will much everything.
	Empty() bool

	// Requirements returns collection of Requirements to expose more selection information.
	Requirements() (requirements Requirements, selectable bool)
}

type SimpleSelector struct {
	requirements []Requirement
}

func NewSelector(requirements ...Requirement) SimpleSelector {
	return SimpleSelector{
		requirements: requirements,
	}
}

func (s *SimpleSelector) Matches(labels Labels) bool {
	for _, requirement := range s.requirements {
		if matches := requirement.Matches(labels); !matches {
			return false
		}
	}

	return true
}

func (s *SimpleSelector) Empty() bool {
	return len(s.requirements) == 0
}

func (s *SimpleSelector) Requirements() (requirements Requirements, selectable bool) {
	return s.requirements, true
}
