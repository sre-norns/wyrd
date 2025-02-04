package manifest_test

import (
	"encoding/json"
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

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
			expect: "key,tier notin (frontend,backend),!role",
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
			expect: "key=value,other_key=xyz,key,tier notin (frontend,backend)",
		},
		"mixed-bag": {
			given: manifest.LabelSelector{
				MatchLabels: manifest.Labels{
					"env":  "dev",
					"tier": "fe",
				},
				MatchSelector: []manifest.SelectorRule{
					{Key: "unit", Op: manifest.LabelSelectorOpExists},
					{Key: "version", Op: manifest.LabelSelectorOpNotIn, Values: []string{"0.9-dev", "0.8-pre"}},
				},
			},
			expect: "env=dev,tier=fe,unit,version notin (0.9-dev,0.8-pre)",
		},
		"mixed-bag-2": {
			given: manifest.LabelSelector{
				MatchLabels: manifest.Labels{
					"env": "dev",
				},
				MatchSelector: []manifest.SelectorRule{
					{Key: "env", Op: manifest.LabelSelectorOpExists},
					{Key: "unit", Op: manifest.LabelSelectorOpDoesNotExist},
					{Key: "version", Op: manifest.LabelSelectorOpNotIn, Values: []string{"0.9", "0.8"}},
					{Key: "phase", Op: manifest.LabelSelectorOpIn, Values: []string{}},
				},
			},
			expect: "env=dev,env,!unit,version notin (0.9,0.8),phase in ()",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := test.given.AsLabels()
			require.Equal(t, test.expect, got)

			_, err := manifest.ParseSelector(got)
			require.NoError(t, err, "Labels are expected to form a valid selector expression")
		})
	}
}

func TesLabelSelector_Parsing(t *testing.T) {
	testCases := map[string]struct {
		expect      manifest.LabelSelector
		given       string
		expectError bool
	}{
		"basic": {
			given: `{"matchLabels":{"os":"linux"},"matchSelector":{"key":"env","operator":"NotIn","values":["dev","testing"]}}`,
			expect: manifest.LabelSelector{
				MatchLabels: manifest.Labels{
					"os": "linux",
				},
				MatchSelector: manifest.SelectorRules{
					{Key: "env", Op: manifest.LabelSelectorOpIn, Values: []string{"dev", "testing"}},
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var got manifest.LabelSelector
			err := json.Unmarshal([]byte(tc.given), &got)

			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "unexpected error")
			}

			require.Equal(t, test.expect, got)
		})
	}
}
