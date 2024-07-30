package manifest

import (
	"sort"
	"strings"
)

type StringSet map[string]struct{}

func NewStringSet(values ...string) StringSet {
	result := make(StringSet, len(values))

	for _, value := range values {
		result[value] = struct{}{}
	}

	return result
}

func (s StringSet) Has(value string) bool {
	_, ok := s[value]
	return ok
}

func (s StringSet) Any() (string, bool) {
	for key := range s {
		return key, true
	}

	var zeroValue string
	return zeroValue, false
}

func (s StringSet) Slice() sort.StringSlice {
	if s == nil {
		return nil
	}

	l := make(sort.StringSlice, 0, len(s))
	for key := range s {
		l = append(l, key)
	}

	return l
}

func (s StringSet) Join(sep string) string {
	return strings.Join(s.Slice(), sep)
}

func (s StringSet) JoinSorted(sep string) string {
	l := s.Slice()
	l.Sort()
	return strings.Join(l, sep)
}
