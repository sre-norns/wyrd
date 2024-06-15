package manifest_test

import (
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func mockRequirement(t *testing.T, key string, op manifest.Operator, values ...string) manifest.Requirement {
	req, err := manifest.NewRequirement(key, op, values)
	require.NoError(t, err)

	return req
}

func TestSimpleSelectorRules(t *testing.T) {
	testCases := map[string]struct {
		given            []manifest.Requirement
		expectEmpty      bool
		expectSelectable bool
		expectRules      []manifest.Requirement
	}{
		"nils": {
			given:            nil,
			expectEmpty:      true,
			expectSelectable: true,
		},
		"empty-req": {
			given:            []manifest.Requirement{},
			expectEmpty:      true,
			expectSelectable: true,
		},

		"req-key-no-key": {
			given: []manifest.Requirement{
				mockRequirement(t, "test-key", manifest.Exists),
			},

			expectEmpty:      false,
			expectSelectable: true,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			selector := manifest.NewSelector(test.given...)
			_, selectable := selector.Requirements()
			require.Equalf(t, test.expectEmpty, selector.Empty(), "Expect empty selector given requirements: %+v", test.given)
			require.Equalf(t, test.expectSelectable, selectable, "Expect selectable given requirements: %+v", test.given)
		})
	}
}

func TestSimpleSelector(t *testing.T) {
	type givenEx struct {
		requirements []manifest.Requirement
		labels       manifest.Labels
	}

	testCases := map[string]struct {
		given  givenEx
		expect bool
	}{
		"nils": {
			given: givenEx{
				requirements: nil,
				labels:       nil,
			},
			expect: true,
		},
		"empty-req": {
			given: givenEx{
				requirements: []manifest.Requirement{},
				labels:       nil,
			},
			expect: true,
		},
		"empty-all": {
			given: givenEx{
				requirements: []manifest.Requirement{},
				labels:       manifest.Labels{},
			},
			expect: true,
		},
		"nil-requirements": {
			given: givenEx{
				requirements: nil,
				labels: manifest.Labels{
					"key":   "value1",
					"key-2": "value1",
				},
			},
			expect: true,
		},

		"req-key-no-key": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.Exists),
				},
				labels: manifest.Labels{},
			},
			expect: false,
		},
		"req-key-has-key": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.Exists),
				},
				labels: manifest.Labels{
					"test-key": "value",
				},
			},
			expect: true,
		},
		"req-key-has-key31": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.DoesNotExist),
				},
				labels: manifest.Labels{
					"test-key": "value",
				},
			},
			expect: false,
		},
		"req-key-has-key33": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.Equals, "value"),
				},
				labels: manifest.Labels{
					"test-key": "value",
				},
			},
			expect: true,
		},
		"req-key-has-key34": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.DoesNotExist, "other-value"),
				},
				labels: manifest.Labels{
					"test-key": "value",
				},
			},
			expect: false,
		},
		"req-union": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.Equals, "value"),
					mockRequirement(t, "test-key", manifest.In, "other-value", "value"),
				},
				labels: manifest.Labels{
					"test-key": "value",
				},
			},
			expect: true,
		},
		"req-exclusion": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.Exists),
					mockRequirement(t, "test-key", manifest.NotIn, "x", "y", "z"),
				},
				labels: manifest.Labels{
					"test-key": "value",
				},
			},
			expect: true,
		},
		"req-range": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.Exists),
					mockRequirement(t, "test-key", manifest.NotEquals, "goo-ga"),
					mockRequirement(t, "test-key", manifest.GreaterThan, "32"),
					mockRequirement(t, "test-key", manifest.LessThan, "35"),
				},
				labels: manifest.Labels{
					"test-key": "33",
				},
			},
			expect: true,
		},
		"req-out-of-range": {
			given: givenEx{
				requirements: []manifest.Requirement{
					mockRequirement(t, "test-key", manifest.Exists),
					mockRequirement(t, "test-key", manifest.GreaterThan, "32"),
					mockRequirement(t, "test-key", manifest.LessThan, "35"),
				},
				labels: manifest.Labels{
					"test-key": "1",
				},
			},
			expect: false,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			selector := manifest.NewSelector(test.given.requirements...)
			// if test.expect.errors {
			// 	require.Error(t, err, "expected error: %v", test.expect.errors)
			// } else {
			// 	require.NoError(t, err, "expected error: %v", test.expect.errors)
			// }
			got := selector.Matches(test.given.labels)
			require.Equalf(t, test.expect, got, "Given labels: %+v, selector: %q", test.given.labels, test.given.requirements)
		})
	}
}
