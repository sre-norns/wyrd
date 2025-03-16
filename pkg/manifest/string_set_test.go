package manifest_test

import (
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestStringSet_Any(t *testing.T) {
	testCases := map[string]struct {
		given    []string
		expectOk bool
		expect   string
	}{
		"some-beans": {
			given:    []string{"one"},
			expectOk: true,
			expect:   "one",
		},
		"nothing": {
			given:    []string{},
			expectOk: false,
			expect:   "",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			set := manifest.NewStringSet(test.given...)
			got, ok := set.Any()
			require.Equal(t, test.expectOk, ok)
			require.Equal(t, test.expect, got)
		})
	}
}

func TestStringSet_Has(t *testing.T) {
	testCases := map[string]struct {
		given  []string
		value  string
		expect bool
	}{
		"empty-set": {
			given:  []string{},
			value:  "test",
			expect: false,
		},
		"some-set": {
			given:  []string{"value", "test", "other"},
			value:  "test",
			expect: true,
		},
		"not-in-set": {
			given:  []string{"value", "test", "other"},
			value:  "needle",
			expect: false,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			set := manifest.NewStringSet(test.given...)
			require.Equal(t, test.expect, set.Has(test.value))
		})
	}
}

func TestStringSet_Join(t *testing.T) {
	testCases := map[string]struct {
		given  []string
		sep    string
		expect string
	}{
		"empty-set": {
			given:  []string{},
			sep:    "+",
			expect: "",
		},
		"single_value": {
			given:  []string{"value"},
			sep:    "+",
			expect: "value",
		},
		"some-set": {
			given:  []string{"value", "test", "other"},
			sep:    "!",
			expect: "value!test!other",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			set := manifest.NewStringSet(test.given...)
			require.Equal(t, test.expect, set.Join(test.sep))
		})
	}
}

func TestStringSet_JoinSorted(t *testing.T) {
	testCases := map[string]struct {
		given  []string
		sep    string
		expect string
	}{
		"empty-set": {
			given:  []string{},
			sep:    "+",
			expect: "",
		},
		"single_value": {
			given:  []string{"value"},
			sep:    "+",
			expect: "value",
		},
		"some-set": {
			given:  []string{"beta", "gama", "alpha"},
			sep:    "!",
			expect: "alpha!beta!gama",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			set := manifest.NewStringSet(test.given...)
			require.Equal(t, test.expect, set.JoinSorted(test.sep))
		})
	}
}
