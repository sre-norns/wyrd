package manifest

import "strings"

type StringSet map[string]struct{}

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

func (s StringSet) Slice() []string {
	if s == nil {
		return nil
	}

	l := make([]string, 0, len(s))
	for key := range s {
		l = append(l, key)
	}

	return l
}

func (s StringSet) Join(sep string) string {
	return strings.Join(s.Slice(), sep)
}
