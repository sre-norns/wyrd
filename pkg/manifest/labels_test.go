package manifest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestLabelsInterface(t *testing.T) {
	require.Zero(t, manifest.Labels{}.Get("key"))
	require.Equal(t, "value", manifest.Labels{"key": "value"}.Get("key"))

	require.Equal(t, false, manifest.Labels{"key": "value"}.Has("key-2"))
	require.Equal(t, false, manifest.Labels{}.Has("key"))
	require.Equal(t, true, manifest.Labels{"key": "value"}.Has("key"))

	require.Empty(t, manifest.Labels{}.Slice())

	var nilLabels manifest.Labels
	require.Zero(t, nilLabels.Slice())
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

func TestLabels_Formatting(t *testing.T) {
	testCases := map[string]struct {
		given  manifest.Labels
		expect string
	}{
		"nil": {
			given:  nil,
			expect: "",
		},
		"empty": {
			given:  manifest.Labels{},
			expect: "",
		},
		"identity": {
			given: manifest.Labels{
				"key": "value",
			},
			expect: "key=value",
		},
		"two": {
			given: manifest.Labels{
				"key-1": "value-1",
				"key-2": "value-2",
			},
			expect: "key-1=value-1,key-2=value-2",
		},
		"mixed-bag": {
			given: manifest.Labels{
				"key-1": "value-1",
				"adsf":  "value-Wooh",
				"betta": "value",
				"ckey":  "0",
			},
			expect: "adsf=value-Wooh,betta=value,ckey=0,key-1=value-1",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(fmt.Sprintf("formatting:%s", name), func(t *testing.T) {
			sb := strings.Builder{}

			test.given.Format(&sb)
			require.EqualValues(t, test.expect, sb.String())
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
		"num-ordering": {
			given: "key < 32, other-key > 3",
			subcases: []subcase{
				{
					// Both keys exist and has value in the range required
					given: manifest.Labels{
						"key":       "16",
						"other-key": "4",
					},
					expect: true,
				},
				{
					// Both keys exist and but only one value is in the range required
					given: manifest.Labels{
						"key":       "16",
						"other-key": "-6",
					},
					expect: false,
				},
				{
					// A single key with valid value exists
					given: manifest.Labels{
						"key": "16",
					},
					expect: false,
				},
				{
					// Both keys exists, but only one has numeric value
					given: manifest.Labels{
						"key":       "16",
						"other-key": "Not-a-number",
					},
					expect: false,
				},
			},
		},
		"key-in-range": {
			given: "key < 32, key > 16",
			subcases: []subcase{
				{ // In range
					given: manifest.Labels{
						"key": "24",
					},
					expect: true,
				},
				{ // Above the range
					given: manifest.Labels{
						"key": "64",
					},
					expect: false,
				},
				{ // Below the range
					given: manifest.Labels{
						"key": "4",
					},
					expect: false,
				},
				{ // No key
					given: manifest.Labels{
						"other-key": "23",
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

			require.Equal(t, test.expect.empty, selector.Empty(), "expect empty selector")

			for _, cas := range test.subcases {
				got := selector.Matches(cas.given)
				require.Equalf(t, cas.expect, got, "Given labels: %+v, selector: %q", cas.given, test.given)
			}
		})
	}
}

func TestValidateSubdmainName(t *testing.T) {
	testCases := map[string]struct {
		given       string
		expectError bool
	}{
		"single-value-ok": {given: "x"},
		"simple-value-ok": {given: "value"},
		"domain.name":     {given: "app.kubernetes.io"},
		"numbers":         {given: "321"},

		"no-negative-numbers":    {given: "-321", expectError: true},
		"no-negative-names":      {given: "-in.k8s.net", expectError: true},
		"no-spaces in between":   {given: "value with spaces", expectError: true},
		"no-spaces at the start": {given: " value", expectError: true},
		"no-spaces at the end":   {given: "value ", expectError: true},

		"no-capitals-start": {given: "Name", expectError: true},
		"no-capitals-mid":   {given: "why-Not", expectError: true},
		"no-capitals-end":   {given: "why.name.X", expectError: true},
		"name-too-long":     {given: "very.long.subdomain.name.very.long.subdomain.name.very.long.subdomain.name.that.is.longer.than.it.should.be.because.it.repeats.long.subdomain.name.that.is.longer.than.it.should.very.long.subdomain.name.that.is.longer.than.it.should.be.because.it.repeats.long.subdomain.name.that.is.longer.than.it.should", expectError: true},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := manifest.ValidateSubdomainName(test.given)
			if test.expectError {
				require.Error(t, got)
			} else {
				require.NoError(t, got)
			}
		})
	}
}
func TestValidateLabelKeyName(t *testing.T) {
	testCases := map[string]struct {
		given       string
		expectError bool
	}{
		"empty": {
			given:       "",
			expectError: true,
		},
		"ok":           {given: "normal"},
		"ok.with.dots": {given: "normal.name"},
		"ok.with.-":    {given: "normal-name"},
		"ok.with.mix":  {given: "0.normal-name"},
		"ok.numbers":   {given: "0.normal"},
		"too-long": {
			given:       "averylong.0000000000000000000000000000000000000000000000000.name.i.have.no-idea.why.anyone-would0.even.type-it.no",
			expectError: true,
		},
		"no-negatives": {
			given:       "-name",
			expectError: true,
		},
		"no-negatives-ends": {
			given:       "name-",
			expectError: true,
		},
		"no-negatives.ends": {
			given:       "name.is-",
			expectError: true,
		},
		"no-+":                {given: "name.+is", expectError: true},
		"no-+start":           {given: "+name", expectError: true},
		"no+end":              {given: "name+", expectError: true},
		"no-space-end":        {given: "name ", expectError: true},
		"no-space_around":     {given: " name ", expectError: true},
		"no-space-in_between": {given: "name with spaces", expectError: true},
		"no-space-start":      {given: " name", expectError: true},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			if test.expectError {
				require.Error(t, manifest.ValidateLabelKeyName(test.given))
			} else {
				require.NoError(t, manifest.ValidateLabelKeyName(test.given))
			}
		})
	}
}

func TestValidateLabelKey(t *testing.T) {
	testCases := map[string]struct {
		given       string
		expectError bool
	}{
		"empty":        {given: "", expectError: true},
		"ok":           {given: "normal"},
		"ok.with.dots": {given: "normal.name"},
		"ok.with.-":    {given: "normal-name"},
		"ok.with.mix":  {given: "0.normal-name"},
		"ok.numbers":   {given: "0.normal"},
		"too-long": {
			given:       "averylong.0000000000000000000000000000000000000000000000000.name.i.have.no-idea.why.anyone-would0.even.type-it.no",
			expectError: true,
		},
		"no-negatives": {
			given:       "-name",
			expectError: true,
		},
		"no-negatives-ends": {
			given:       "name-",
			expectError: true,
		},
		"no-negatives.ends": {
			given:       "name.is-",
			expectError: true,
		},
		"no-+":                {given: "name.+is", expectError: true},
		"no-+start":           {given: "+name", expectError: true},
		"no+end":              {given: "name+", expectError: true},
		"no-space-end":        {given: "name ", expectError: true},
		"no-space_around":     {given: " name ", expectError: true},
		"no-space-in_between": {given: "name with spaces", expectError: true},
		"no-space-start":      {given: " name", expectError: true},

		"simple/prefix":       {given: "prefix/name"},
		"domain/prefix":       {given: "example.prefix.io/name"},
		"domain/prefix+.name": {given: "example.prefix.io/name.meta"},

		"simple/+prefix":  {given: "prefix/+name", expectError: true},
		"domain/prefix+":  {given: "example.prefix.io/name+", expectError: true},
		"domain/too-long": {given: "example.prefix.io/averylong.0000000000000000000000000000000000000000000000000.name.i.have.no-idea.why.anyone-would0.even.type-it.no", expectError: true},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			if test.expectError {
				require.Error(t, manifest.ValidateLabelKey(test.given))
			} else {
				require.NoError(t, manifest.ValidateLabelKey(test.given))
			}
		})
	}
}

func TestValidateLabelValue(t *testing.T) {
	testCases := map[string]struct {
		given       string
		expectError bool
	}{
		"empty-value-ok":  {given: ""},
		"simple-value-ok": {given: "value"},
		"domain.name":     {given: "app.kubernetes.io"},
		"numbers":         {given: "321"},
		"version.1":       {given: "v8"},
		"version.2":       {given: "v8.3"},
		"version.3":       {given: "v.8.3"},
		"single-number":   {given: "3"},

		"no-negative-numbers?":   {given: "-321", expectError: true},
		"no-spaces in between":   {given: "value with spaces", expectError: true},
		"no-spaces at the start": {given: " value", expectError: true},
		"no-spaces at the end":   {given: "value ", expectError: true},
	}

	for k, tc := range testCases {
		test := tc
		name := k
		t.Run(name, func(t *testing.T) {
			if test.expectError {
				require.Error(t, manifest.ValidateLabelValue(name, test.given))
			} else {
				require.NoError(t, manifest.ValidateLabelValue(name, test.given))
			}
		})
	}
}

func TestValidateLabels(t *testing.T) {
	testCases := map[string]struct {
		given       manifest.Labels
		expectError bool
	}{
		"nil-value-valid": {given: nil},
		"empty-value-ok": {
			given: manifest.Labels{},
		},
		"just.key": {
			given: manifest.Labels{
				"key":                 "",
				"app.k8s.io/key.name": "",
			},
		},
		"numbers": {
			given: manifest.Labels{
				"app.k8s.io/version":                "321",
				"app.k8s.io/version.1":              "321",
				"app.k8s.io/version.semantic":       "1.2.3",
				"app.k8s.io/version.semantic.build": "1.2.3-dev",
			},
		},

		"space.values": {
			given: manifest.Labels{
				"key": "value with spaces",
			},
			expectError: true,
		},
		"partially-valid": {
			given: manifest.Labels{
				"key1": "valid",
				"key":  "+invalid value",
			},
			expectError: true,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			if test.expectError {
				require.Error(t, test.given.Validate())
			} else {
				require.NoError(t, test.given.Validate())
			}
		})
	}
}
