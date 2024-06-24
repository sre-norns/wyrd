package manifest

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Labels represent a set of key-value pairs associated with a resource.
// Interface is intensionally compatible with [k8s.io/apimachinery/pkg/labels.Set]
type Labels map[string]string

// Has checks if a given key is present in labels set.
func (l Labels) Has(key string) bool {
	_, ok := l[key]
	return ok
}

// Get returns label value of a given key.
// It returns string nil value - empty string - if the key is not in the labels set.
func (l Labels) Get(key string) string {
	return l[key]
}

func (l Labels) Slice() sort.StringSlice {
	if l == nil {
		return nil
	}

	labels := make(sort.StringSlice, 0, len(l))
	for r := range l {
		labels = append(labels, r)
	}

	return labels
}

var labelRegexp = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_\.\-]*[a-zA-Z0-9]$`)

func ValidateLabelKeyName(value string) error {
	if value == "" {
		return errors.New("key name can not be empty")
	}

	if len(value) > 63 {
		return errors.New("key name is too long")
	}

	if !labelRegexp.MatchString(value) {
		return errors.New("key `name` is not valid")
	}

	return nil
}

func ValidateLabelKeyPrefix(value string) error {
	if len(value) > 253 {
		return errors.New("key prefix is too long")
	}

	return nil
}

func ValidateLabelKey(value string) error {
	if value == "" {
		return errors.New("value can not be empty")
	}

	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return errors.New("key can not contain extra '/' in the name")
	}

	if len(parts) == 2 {
		invalidPrefix := ValidateLabelKeyPrefix(parts[0])
		invalidName := ValidateLabelKeyName(parts[1])
		if invalidPrefix != nil && invalidName == nil {
			return invalidPrefix
		} else if invalidPrefix == nil && invalidName != nil {
			return invalidName
		} else if invalidPrefix != nil && invalidName != nil {
			return fmt.Errorf("%w And %v", invalidPrefix, invalidName)
		}

		return nil
	}

	return ValidateLabelKeyName(parts[0])
}

func ValidateLabelValue(value string) error {
	// Empty value is valid
	if value == "" {
		return nil
	}

	if len(value) > 63 {
		return errors.New("value is too long")
	}

	if !labelRegexp.MatchString(value) {
		return errors.New("value is not valid")
	}

	return nil
}

func (l Labels) Validate() error {
	errs := []error{}
	for key, value := range l {
		if err := ValidateLabelKey(key); err != nil {
			errs = append(errs, err)
		}
		if err := ValidateLabelValue(value); err != nil {
			errs = append(errs, err)
		}
	}

	return AsMultiErrorOrNil(errs...)
}

// Format writes string representation of the [SelectorRule] into the provided sb.
func (l Labels) Format(sb *strings.Builder) {
	labelsKey := l.Slice()
	// Provides stable order for keys in the map
	labelsKey.Sort()

	// Iterate over the keys in a sorted order
	for i, key := range labelsKey {
		value := l[key]
		if i != 0 {
			sb.WriteRune(',')
		}
		sb.WriteString(key)
		sb.WriteString("=")
		sb.WriteString(value)
	}
}

// MergeLabels returns a new [Labels] that is a union of labels passed.
func MergeLabels(labels ...Labels) Labels {
	count := 0
	for _, l := range labels {
		count += len(l)
	}

	result := make(Labels, count)
	for _, l := range labels {
		for k, v := range l {
			result[k] = v
		}
	}

	return result
}

// LabelSelectorOperator defines a type to represent operator for label selector.
type LabelSelectorOperator string

const (
	LabelSelectorOpIn           LabelSelectorOperator = "In"
	LabelSelectorOpNotIn        LabelSelectorOperator = "NotIn"
	LabelSelectorOpExists       LabelSelectorOperator = "Exists"
	LabelSelectorOpDoesNotExist LabelSelectorOperator = "DoesNotExist"
)

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
