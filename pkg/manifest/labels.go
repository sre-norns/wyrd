package manifest

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrNameIsEmpty = errors.New("name is empty")

	ErrKeyNameEmpty   = fmt.Errorf("key %w", ErrNameIsEmpty)
	ErrKeyNameTooLong = fmt.Errorf("key %w", ErrNameTooLong)

	ErrLabelValueTooLong = errors.New("label value is too long")
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

var labelRegexp = regexp.MustCompile(`^[[:alnum:]]$|^[a-zA-Z0-9][a-zA-Z0-9_\.\-]*[a-zA-Z0-9]$`)

func ValidateLabelKeyName(value string) error {
	if value == "" {
		return ErrKeyNameEmpty
	}

	if len(value) > 63 {
		return ErrKeyNameTooLong
	}

	if !labelRegexp.MatchString(value) {
		return errors.New("key `name` is not valid")
	}

	return nil
}

func ValidateLabelKeyPrefix(value string) error {
	return ValidateSubdomainName(value)
}

func ValidateLabelKey(value string) error {
	if value == "" {
		return ErrKeyNameEmpty
	}

	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return fmt.Errorf("label key %v can not contain extra '/' in the name", value)
	} else if len(parts) == 2 {
		invalidPrefix := ValidateLabelKeyPrefix(parts[0])
		invalidName := ValidateLabelKeyName(parts[1])

		if invalidPrefix != nil && invalidName == nil {
			return fmt.Errorf("label %v prefix %w", value, invalidPrefix)
		} else if invalidPrefix == nil && invalidName != nil {
			return fmt.Errorf("label %v name-part %w", value, invalidName)
		} else if invalidPrefix != nil && invalidName != nil {
			return fmt.Errorf("label %v: prefix %v AND name-part %v", value, invalidPrefix, invalidName)
		}

		return nil
	}

	if invalidName := ValidateLabelKeyName(parts[0]); invalidName != nil {
		return fmt.Errorf("label %v name-part %w", value, invalidName)
	}

	return nil
}

func ValidateLabelValue(key, value string) error {
	// Empty value is valid
	if value == "" {
		return nil
	}

	if len(value) > 63 {
		return fmt.Errorf("%v %w", key, ErrLabelValueTooLong)
	}

	if !labelRegexp.MatchString(value) {
		return fmt.Errorf("label %q value is not valid, doesn't match regex %q", key, labelRegexp)
	}

	return nil
}

func (l Labels) Validate() error {
	errs := ErrorSet{}
	for key, value := range l {
		if err := ValidateLabelKey(key); err != nil {
			errs = append(errs, err)
		}
		if err := ValidateLabelValue(key, value); err != nil {
			errs = append(errs, err)
		}
	}

	return errs.ErrorOrNil()
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
