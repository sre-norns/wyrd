package manifest

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Operator string

const (
	Exists       Operator = "exists"
	DoesNotExist Operator = "!"
	Equals       Operator = "="
	DoubleEquals Operator = "=="
	In           Operator = "in"
	NotEquals    Operator = "!="
	NotIn        Operator = "notin"

	GreaterThan Operator = "gt"
	LessThan    Operator = "lt"
)

var (
	ErrInvalidOperator           = errors.New("invalid operator")
	ErrNonSelectableRequirements = errors.New("non-selectable requirements")
)

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

func NewRequirement(key string, op Operator, values []string) (Requirement, error) {
	// TODO: Validate key
	if !IsValidOperator(string(op)) {
		return Requirement{}, fmt.Errorf("%w: %v", ErrInvalidOperator, op)
	}

	return Requirement{
		key:      key,
		operator: op,
		values:   NewStringSet(values...),
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

func (r *Requirement) String() string {
	if r == nil {
		return "<Requirement:nil>"
	}

	if len(r.key) == 0 {
		return ""
	}

	var sb strings.Builder
	switch r.operator {
	case Exists:
		sb.WriteString(r.key)
	case DoesNotExist:
		sb.WriteString(string(DoesNotExist))
		sb.WriteString(r.key)
	case Equals:
		sb.WriteString(fmt.Sprintf("%v%v%v", r.key, Equals, r.Values().Slice()[0]))
	case DoubleEquals:
		sb.WriteString(fmt.Sprintf("%v%v%v", r.key, DoubleEquals, r.Values().Slice()[0]))
	case NotEquals:
		sb.WriteString(fmt.Sprintf("%v%v%v", r.key, NotEquals, r.Values().Slice()[0]))

	case GreaterThan:
		sb.WriteString(fmt.Sprintf("%v>%v", r.key, r.Values().Slice()[0]))
	case LessThan:
		sb.WriteString(fmt.Sprintf("%v<%v", r.key, r.Values().Slice()[0]))

	case In:
		sb.WriteString(fmt.Sprintf("%v %v (%v)", r.key, In, r.Values().JoinSorted(",")))
	case NotIn:
		sb.WriteString(fmt.Sprintf("%v %v (%v)", r.key, NotIn, r.Values().JoinSorted(",")))
	default:
		sb.WriteString(fmt.Sprintf("%v %v (%v)", r.key, r.operator, r.Values().JoinSorted(",")))
	}

	return sb.String()
}

// Selector is an interface for objects that can apply rules to match [Labels]
type Selector interface {
	// Matches returns true if the selector matches given label set.
	Matches(labels Labels) bool

	// Empty returns true if the selector is empty and thus will much everything.
	Empty() bool

	// Requirements returns collection of Requirements to expose more selection information.
	Requirements() (requirements Requirements, selectable bool)

	String() string
}

type SimpleSelector struct {
	requirements Requirements
}

func NewSelector(requirements ...Requirement) *SimpleSelector {
	return &SimpleSelector{
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

func (s *SimpleSelector) String() string {
	reqs := make([]string, 0, len(s.requirements))
	for _, req := range s.requirements {
		reqs = append(reqs, req.String())
	}
	return strings.Join(reqs, ",")
}
