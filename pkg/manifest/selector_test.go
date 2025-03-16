package manifest_test

import (
	"testing"
	"time"

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
			got := selector.Matches(test.given.labels)
			require.Equalf(t, test.expect, got, "Given labels: %+v, selector: %q", test.given.labels, test.given.requirements)
		})
	}
}

func Test_RequirementStringRepr(t *testing.T) {
	testCases := map[string]struct {
		given  manifest.Requirement
		expect string
	}{
		"nils": {
			given:  manifest.Requirement{},
			expect: "",
		},
		"exists": {
			given:  mockRequirement(t, "test-key", manifest.Exists),
			expect: "test-key",
		},
		"!exists": {
			given:  mockRequirement(t, "mykey", manifest.DoesNotExist),
			expect: "!mykey",
		},
		"eq": {
			given:  mockRequirement(t, "mykey", manifest.Equals, "value1"),
			expect: "mykey=value1",
		},
		"!eq": {
			given:  mockRequirement(t, "mykey", manifest.NotEquals, "value1"),
			expect: "mykey!=value1",
		},
		"in": {
			given:  mockRequirement(t, "version", manifest.In, "0.9-dev", "0.8-pre"),
			expect: "version in (0.8-pre,0.9-dev)",
		},
		"!in": {
			given:  mockRequirement(t, "env", manifest.NotIn, "0.9-dev", "0.8-pre", "debug"),
			expect: "env notin (0.8-pre,0.9-dev,debug)",
		},
		"gt": {
			given:  mockRequirement(t, "number", manifest.GreaterThan, "431"),
			expect: "number>431",
		},
		"lt": {
			given:  mockRequirement(t, "number", manifest.LessThan, "974"),
			expect: "number<974",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.expect, test.given.String())
		})
	}
}

func Test_SelectorCanParseToString(t *testing.T) {
	testCases := map[string]struct {
		given       *manifest.SimpleSelector
		expectError bool
	}{
		"nils": {
			given: manifest.NewSelector(),
		},
		"exists": {
			given: manifest.NewSelector(
				mockRequirement(t, "test-key", manifest.Exists),
			),
		},
		"!exists": {
			given: manifest.NewSelector(
				mockRequirement(t, "test-key", manifest.Exists),
				mockRequirement(t, "mykey", manifest.DoesNotExist),
			),
		},
		"eq": {
			given: manifest.NewSelector(
				mockRequirement(t, "mykey2", manifest.Equals, "value1"),
				mockRequirement(t, "test-key", manifest.Exists),
				mockRequirement(t, "mykey", manifest.DoesNotExist),
			),
		},
		"in-all": {
			given: manifest.NewSelector(
				mockRequirement(t, "mykey2", manifest.Equals, "value1"),
				mockRequirement(t, "test-key", manifest.Exists),
				mockRequirement(t, "mykey", manifest.DoesNotExist),
				mockRequirement(t, "version", manifest.In, "0.9-dev", "0.8-pre"),
				mockRequirement(t, "mykey", manifest.NotEquals, "value1"),
				mockRequirement(t, "number", manifest.LessThan, "974"),
				mockRequirement(t, "number", manifest.GreaterThan, "431"),
			),
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			_, err := manifest.ParseSelector(test.given.String())
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
			}
		})
	}
}

func TestSearchQuery_IsEmpty(t *testing.T) {
	testCases := map[string]struct {
		given  manifest.SearchQuery
		expect bool
	}{
		"empty-query-isempty": {
			given:  manifest.SearchQuery{},
			expect: true,
		},
		"empty-query-selector-isempty": {
			given: manifest.SearchQuery{
				Selector: manifest.NewSelector(),
			},
			expect: true,
		},

		"limited-query-is-not-empty": {
			given: manifest.SearchQuery{
				Limit: 150,
			},
		},
		"paginated-query-is-not-empty": {
			given: manifest.SearchQuery{
				Offset: 150,
				Limit:  50,
			},
		},

		"time-query-is-not-empty": {
			given: manifest.SearchQuery{
				FromTime: time.Date(2005, time.June, 3, 0, 0, 0, 0, time.Local),
			},
		},
		"timerange-query-is-not-empty": {
			given: manifest.SearchQuery{
				FromTime: time.Date(2005, time.June, 3, 0, 0, 0, 0, time.Local),
				TillTime: time.Date(2015, time.February, 3, 0, 0, 0, 0, time.Local),
			},
		},
		"name-query-is-not-empty": {
			given: manifest.SearchQuery{
				Name: "needle",
			},
		},

		"query-with-selector-not-empty": {
			given: manifest.SearchQuery{
				Selector: manifest.NewSelector(
					mockRequirement(t, "test-key", manifest.Exists),
				),
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.expect, test.given.Empty())
		})
	}
}
