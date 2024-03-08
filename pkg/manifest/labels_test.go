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
				MatchSelector: []manifest.Selector{
					{Key: "key", Op: manifest.LabelSelectorOpExists, Values: []string{"bogus"}},
				},
			},
			expect: "key",
		},

		"keys-multy": {
			given: manifest.LabelSelector{
				MatchSelector: []manifest.Selector{
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
				MatchSelector: []manifest.Selector{
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
