package bark_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func Test_NewErrorResponse(t *testing.T) {
	type givenT struct {
		Status  int
		Error   error
		Options []bark.HResponseOption
	}

	testCases := map[string]struct {
		given  givenT
		expect *bark.ErrorResponse
	}{
		"base": {
			given: givenT{
				Status: 1,
				Error:  fmt.Errorf("hello"),
			},
			expect: &bark.ErrorResponse{
				Code:    1,
				Message: "hello",
			},
		},
		"nil-error-wo-options": {
			given: givenT{
				Status: 200,
				Error:  nil,
			},
			expect: nil,
		},
		"nil-error-w-options": {
			given: givenT{
				Status: 200,
				Error:  nil,
				Options: []bark.HResponseOption{
					bark.WithLink("details", manifest.HLink{Reference: "321", Relationship: "xyz"}),
				},
			},
			expect: &bark.ErrorResponse{
				Code:    200,
				Message: "",

				HResponse: manifest.HResponse{
					Links: map[string]manifest.HLink{
						"details": {
							Reference:    "321",
							Relationship: "xyz",
						},
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := bark.NewErrorResponse(test.given.Status, test.given.Error, test.given.Options...)

			require.Equal(t, test.expect, got)
		})
	}
}

func TestPagination_ClampLimit(t *testing.T) {
	testCases := map[string]struct {
		given      bark.Pagination
		givenLimit uint
		expect     bark.Pagination
	}{
		"all-defaults": {
			given:      bark.Pagination{},
			givenLimit: 0,
			expect:     bark.Pagination{},
		},
		"default-limit": {
			given: bark.Pagination{
				Page: 120,
			},
			givenLimit: 32,
			expect: bark.Pagination{
				Page:     120,
				PageSize: 32,
			},
		},
		"request-above-limit": {
			given: bark.Pagination{
				Page:     0,
				PageSize: 2000,
			},
			givenLimit: 100,
			expect: bark.Pagination{
				Page:     0,
				PageSize: 100,
			},
		},
		"given-under-limit": {
			given: bark.Pagination{
				Page:     0,
				PageSize: 100,
			},
			givenLimit: 2000,
			expect: bark.Pagination{
				Page:     0,
				PageSize: 100,
			},
		},
		"limit-zero": {
			given: bark.Pagination{
				Page:     32,
				PageSize: 2000,
			},
			givenLimit: 0,
			expect: bark.Pagination{
				Page:     32,
				PageSize: 0,
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := test.given.ClampLimit(test.givenLimit)

			require.Equal(t, test.expect, got)
		})
	}
}

func TestNewPaginatedResponse(t *testing.T) {
	type testValue struct {
		value string
	}
	type paginationInputs struct {
		items          []testValue
		totalCount     int64
		paginationInfo bark.Pagination
		options        []bark.HResponseOption
	}

	testCases := map[string]struct {
		given  paginationInputs
		expect bark.PaginatedResponse[testValue]
	}{
		"nil-value": {
			given:  paginationInputs{},
			expect: bark.PaginatedResponse[testValue]{},
		},
		"nil-value-with-options": {
			given: paginationInputs{
				options: []bark.HResponseOption{
					bark.WithLink("self", manifest.HLink{
						Reference:    "location",
						Relationship: "?",
					},
					),
				},
			},
			expect: bark.PaginatedResponse[testValue]{
				HResponse: manifest.HResponse{
					Links: map[string]manifest.HLink{
						"self": {
							Reference:    "location",
							Relationship: "?",
						},
					},
				},
			},
		},
		"collection-pages": {
			given: paginationInputs{
				totalCount: 100500,
				items: []testValue{
					{value: "1"},
					{value: "something"},
				},
			},
			expect: bark.PaginatedResponse[testValue]{
				Total: 100500,
				Count: 2,
				Data: []testValue{
					{value: "1"},
					{value: "something"},
				},
			},
		},
		"collection-with-options": {
			given: paginationInputs{
				items: []testValue{
					{value: "1"},
					{value: "something"},
				},
				options: []bark.HResponseOption{
					bark.WithLink("self", manifest.HLink{
						Reference:    "location",
						Relationship: "?",
					},
					),
				},
			},
			expect: bark.PaginatedResponse[testValue]{
				Count: 2,
				Data: []testValue{
					{value: "1"},
					{value: "something"},
				},
				HResponse: manifest.HResponse{
					Links: map[string]manifest.HLink{
						"self": {
							Reference:    "location",
							Relationship: "?",
						},
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := bark.NewPaginatedResponse(test.given.items, test.given.totalCount, test.given.paginationInfo, test.given.options...)

			require.Equal(t, test.expect, got)
		})
	}
}

func TestSearchTimeRange(t *testing.T) {
	// baseTime := time.Time{}

	emptySelector, err := manifest.ParseSelector("")
	if err != nil {
		t.Fatalf("failed to setup empty selector for test: %v", err)
	}

	testCases := map[string]struct {
		given                bark.SearchParams
		givenDefaultPageSize uint

		expect      manifest.SearchQuery
		expectError bool
	}{
		"nil-range": {
			expect: manifest.SearchQuery{
				Selector: emptySelector,
			},
		},
		"just-name": {
			givenDefaultPageSize: 25,
			given: bark.SearchParams{
				Name: "xyz",
			},
			expect: manifest.SearchQuery{
				Selector: emptySelector,
				Name:     "xyz",
				Limit:    25,
			},
		},

		"invalid-label-selector": {
			givenDefaultPageSize: 25,
			given: bark.SearchParams{
				Filter: "xyz Like this",
			},
			expectError: true,
		},

		"time-range-absolute": {
			givenDefaultPageSize: 25,
			given: bark.SearchParams{
				Timerange: bark.Timerange{
					FromTime: "2024-02-27",
					TillTime: "2024-02-27 13:44:15",
				},
			},
			expect: manifest.SearchQuery{
				Selector: emptySelector,
				FromTime: time.Date(2024, 02, 27, 0, 0, 0, 0, time.Local),
				TillTime: time.Date(2024, 02, 27, 13, 44, 15, 0, time.Local),
				Limit:    25,
			},
		},

		"invalid-range-absolute": {
			givenDefaultPageSize: 25,
			given: bark.SearchParams{
				Timerange: bark.Timerange{
					FromTime: "2024-02-28",
					TillTime: "2024-02-27 13:44:15",
				},
			},
			expectError: true,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := test.given.BuildQuery(test.givenDefaultPageSize)

			if test.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, test.expect, got)
		})
	}

}
