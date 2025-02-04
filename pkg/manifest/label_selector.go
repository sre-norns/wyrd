package manifest

import (
	"fmt"
	"strings"
)

// LabelSelectorOperator defines a type to represent operator for label selector.
type LabelSelectorOperator string

const (
	LabelSelectorOpIn           LabelSelectorOperator = "In"
	LabelSelectorOpNotIn        LabelSelectorOperator = "NotIn"
	LabelSelectorOpExists       LabelSelectorOperator = "Exists"
	LabelSelectorOpDoesNotExist LabelSelectorOperator = "DoesNotExist"
)

var labelSelectorToSelectorOp = map[LabelSelectorOperator]Operator{
	LabelSelectorOpIn:           In,
	LabelSelectorOpNotIn:        NotIn,
	LabelSelectorOpExists:       Exists,
	LabelSelectorOpDoesNotExist: DoesNotExist,
}

func (op LabelSelectorOperator) ToSelectorOp() (Operator, error) {
	r, ok := labelSelectorToSelectorOp[op]
	if !ok {
		return "", fmt.Errorf("unexpected LabelSelectorOperator %q has no equivalent Selector Operator", op)
	}

	return r, nil
}

// SelectorRule represents a single math-rule used by [LabelSelector] type to matching [Labels].
// Nil value doesn't match anything.
type SelectorRule struct {
	// Key is the name of the key to select
	Key string `json:"key,omitempty" yaml:"key,omitempty" `
	// Op is a math operation to perform. See [LabelSelectorOperator] doc for more info.
	Op LabelSelectorOperator `json:"operator,omitempty" yaml:"operator,omitempty" `
	// Values is an optional list of value to apply [SelectorRule.Op] to. For Operator like [Exist] the list must be empty.
	Values []string `json:"values,omitempty" yaml:"values,omitempty" `
}

type SelectorRules []SelectorRule

// Format writes string representation of the [SelectorRule]s into the provided string builder sb.
func (s SelectorRule) Format(sb *strings.Builder) {
	switch s.Op {
	case LabelSelectorOpExists:
		sb.WriteString(s.Key)
	case LabelSelectorOpDoesNotExist:
		sb.WriteString(string(DoesNotExist))
		sb.WriteString(s.Key)
	case LabelSelectorOpIn:
		sb.WriteString(fmt.Sprintf("%v %v (%v)", s.Key, In, strings.Join(s.Values, ",")))
	case LabelSelectorOpNotIn:
		sb.WriteString(fmt.Sprintf("%v %v (%v)", s.Key, NotIn, strings.Join(s.Values, ",")))
	default:
		sb.WriteString(fmt.Sprintf("%v %v (%v)", s.Key, s.Op, strings.Join(s.Values, ",")))
	}
}

func (s SelectorRule) String() string {
	sb := strings.Builder{}
	s.Format(&sb)
	return sb.String()
}

// LabelSelector is a part of a resource model that holds label-based requirements for another resource
type LabelSelector struct {
	MatchLabels Labels `json:"matchLabels,omitempty" yaml:"matchLabels,omitempty" `

	MatchSelector SelectorRules `json:"matchSelector,omitempty" yaml:"matchSelector,omitempty" `
}

// AsLabels returns string representation of the [LabelSelector] or an error.
// All [LabelSelector.MatchLabels] converted into exact 'equals' operation.
// All [LabelSelector.MatchSelector] converted into respective representation.
//
// For example:
// ```go
//
//		LabelSelector{
//			MatchLabels: Labels {
//				"env": "dev",
//				"tier": "fe",
//			},
//
//			MatchSelector: []SelectorRule {
//				{ Key: "unit", Op: LabelSelectorOpExists },
//				{ Key: "version", Op: LabelSelectorOpNotIn, Values: []string{"0.9-dev", "0.8-pre"} },
//			},
//	    }.AsLabels()
//
// ```
//
// Produces:
// ```
// "env=dev,tier=fe,unit,version notin (0.9-dev,0.8-pre)"
// ```
func (ls LabelSelector) AsLabels() string {
	sb := strings.Builder{}
	ls.MatchLabels.Format(&sb)

	for _, s := range ls.MatchSelector {
		if sb.Len() > 0 {
			sb.WriteRune(',')
		}

		s.Format(&sb)
	}

	return sb.String()
}

func (ls LabelSelector) String() string {
	return ls.AsLabels()
}

func (ls LabelSelector) AsSelector() (Selector, error) {
	// labelsExpr := ls.AsLabels()
	// return ParseSelector(labelsExpr)

	req := make(Requirements, 0, len(ls.MatchLabels)+len(ls.MatchSelector))
	for label, value := range ls.MatchLabels {
		r, err := NewRequirement(label, Equals, []string{value})
		if err != nil {
			return nil, fmt.Errorf("failed to create a requirement for MatchLabels key=%q, value=%q: %w", label, value, err)
		}
		req = append(req, r)
	}

	for _, rule := range ls.MatchSelector {
		op, err := rule.Op.ToSelectorOp()
		if err != nil {
			return nil, err
		}

		r, err := NewRequirement(rule.Key, op, rule.Values)
		if err != nil {
			return nil, fmt.Errorf("failed to create a requirement for MatchLabels key=%q: %w", rule.Key, err)
		}
		req = append(req, r)
	}

	return NewSelector(req...), nil
}
