package manifest_test

import (
	"fmt"
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestLabesInterface(t *testing.T) {
	require.Equal(t, "", manifest.Labels{}.Get("key"))
	require.Equal(t, "value", manifest.Labels{"key": "value"}.Get("key"))

	require.Equal(t, false, manifest.Labels{"key": "value"}.Has("key-2"))
	require.Equal(t, false, manifest.Labels{}.Has("key"))
	require.Equal(t, true, manifest.Labels{"key": "value"}.Has("key"))
}

func TestLabels_Merging(t *testing.T) {
	testCases := map[string]struct {
		given  []manifest.Labels
		expect manifest.Labels
	}{
		"nil": {
			given:  []manifest.Labels{},
			expect: manifest.Labels{},
		},
		"identity": {
			given: []manifest.Labels{
				{"key": "value"},
			},
			expect: manifest.Labels{"key": "value"},
		},
		"two": {
			given: []manifest.Labels{
				{"key-1": "value-1"},
				{"key-2": "value-2"},
			},
			expect: manifest.Labels{
				"key-1": "value-1",
				"key-2": "value-2",
			},
		},
		"key-override": {
			given: []manifest.Labels{
				{"key-1": "value-1", "key-2": "value-2"},
				{"key-2": "value-Wooh"},
			},
			expect: manifest.Labels{
				"key-1": "value-1",
				"key-2": "value-Wooh",
			},
		},
		"mixed-bag": {
			given: []manifest.Labels{
				{"key-1": "value-1", "key-2": "value-2"},
				{"key-2": "value-Wooh", "key-3": "value-3"},
				{"key-2": "value-Naah", "key-4": "value-3"},
			},
			expect: manifest.Labels{
				"key-1": "value-1",
				"key-2": "value-Naah",
				"key-3": "value-3",
				"key-4": "value-3",
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(fmt.Sprintf("merging:%s", name), func(t *testing.T) {
			got := manifest.MergeLabels(test.given...)
			require.EqualValues(t, test.expect, got)
		})
	}
}

func TestLabelSelector_AsLabels(t *testing.T) {
	testCases := map[string]struct {
		given       manifest.LabelSelector
		expect      string
		expectError bool
	}{
		"empty": {
			given:  manifest.LabelSelector{},
			expect: "",
		},
		"labels-only-1": {
			given: manifest.LabelSelector{
				MatchLabels: manifest.Labels{
					"key": "value",
				},
			},
			expect: "key=value",
		},
		"labels-only-2": {
			given: manifest.LabelSelector{
				MatchLabels: manifest.Labels{
					"environment": "production",
					"tier":        "frontend",
				},
			},
			expect: "environment=production,tier=frontend",
		},
		"key-exist": {
			given: manifest.LabelSelector{
				MatchSelector: []manifest.SelectorRule{
					{Key: "key", Op: manifest.LabelSelectorOpExists, Values: []string{"bogus"}},
				},
			},
			expect: "key",
		},

		"keys-multy": {
			given: manifest.LabelSelector{
				MatchSelector: []manifest.SelectorRule{
					{Key: "key", Op: manifest.LabelSelectorOpExists, Values: []string{"bogus"}},
					{Key: "tier", Op: manifest.LabelSelectorOpNotIn, Values: []string{"frontend", "backend"}},
					{Key: "role", Op: manifest.LabelSelectorOpDoesNotExist},
				},
			},
			expect: "key,tier notin (frontend, backend),!role",
		},

		"keys-mix": {
			given: manifest.LabelSelector{
				MatchLabels: manifest.Labels{
					"key":       "value",
					"other_key": "xyz",
				},
				MatchSelector: []manifest.SelectorRule{
					{Key: "key", Op: manifest.LabelSelectorOpExists, Values: []string{"bogus"}},
					{Key: "tier", Op: manifest.LabelSelectorOpNotIn, Values: []string{"frontend", "backend"}},
				},
			},
			expect: "key=value,other_key=xyz,key,tier notin (frontend, backend)",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := test.given.AsLabels()
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}

func TestParseSelector(t *testing.T) {
	type subcase struct {
		given  manifest.Labels
		expect bool
	}

	type parseExpectations struct {
		errors bool
		empty  bool
	}

	testCases := map[string]struct {
		given    string
		expect   parseExpectations
		subcases []subcase
	}{
		"empty-selector": {
			given: "",
			expect: parseExpectations{
				empty: true,
			},
			subcases: []subcase{
				{
					given:  manifest.Labels{},
					expect: true,
				},
				{
					given: manifest.Labels{
						"key": "value",
					},
					expect: true,
				},
			},
		},
		"doc-example-in": {
			given: "key in (value1, value2)",
			subcases: []subcase{
				{
					given: manifest.Labels{
						"key":   "value1",
						"key-2": "value1",
					},
					expect: true,
				},
				{
					given: manifest.Labels{
						"key": "value1",
					},
					expect: true,
				},
				{
					given: manifest.Labels{
						"key": "value",
					},
					expect: false,
				},
				{
					given: manifest.Labels{
						"key-2": "value1",
					},
					expect: false,
				},
			},
		},
		"doc-notIn-example": {
			given: "key notin (value1, value2)",
			subcases: []subcase{
				{ // Exclusivity of `notIn` - key value not equal to any in the list or key doesn't exist
					// In this case we check that the key doesn't exist
					given: manifest.Labels{
						"other": "value",
					},
					expect: true,
				},
				{
					// In this case we check that the key exists but value not in the list
					given: manifest.Labels{
						"key": "value",
					},
					expect: true,
				},
				{
					// Key exist and value is in the list, thus not-match
					given: manifest.Labels{
						"key": "value1",
					},
					expect: false,
				},
			},
		},
		"doc-example-complex": {
			given: "x in (foo,,baz),y,z notin ()",
			subcases: []subcase{
				{
					given: manifest.Labels{
						"x": "foo",
						"y": "doesn't matter",
						"w": "no one cares",
					},
					expect: true,
				},
				{
					given: manifest.Labels{
						"x": "",
						"y": "doesn't matter",
						"w": "no one cares",
					},
					expect: true,
				},
				{
					given: manifest.Labels{
						"w": "no one cares",
						"x": "foo",
						"y": "doesn't matter",
						"z": "Any value but empty",
					},
					expect: true,
				},
				{
					given: manifest.Labels{
						"w": "no one cares",
						"x": "foo",
						"y": "doesn't matter",
						"z": "",
					},
					expect: false,
				},
				{
					// In this case we check that the key exists but value not in the list
					given: manifest.Labels{
						"key": "value",
					},
					expect: false,
				},
				{
					given: manifest.Labels{
						"x": "something else",
						"y": "doesn't matter",
					},
					expect: false,
				},
				{
					given: manifest.Labels{
						"x": "something else",
						"y": "doesn't matter",
					},
					expect: false,
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			selector, err := manifest.ParseSelector(test.given)
			if test.expect.errors {
				require.Error(t, err, "expected error: %v", test.expect.errors)
			} else {
				require.NoError(t, err, "expected error: %v", test.expect.errors)
			}

			if test.expect.empty {
				require.True(t, selector.Empty(), "expect empty selector")
			}

			for _, cas := range test.subcases {
				got := selector.Matches(cas.given)
				require.Equalf(t, cas.expect, got, "Given labels: %+v, selector: %q", cas.given, test.given)
			}
		})
	}
}
