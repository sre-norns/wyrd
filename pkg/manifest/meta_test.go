package manifest_test

import (
	"encoding/json"
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestManifestMarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}

	testCases := map[string]struct {
		given       manifest.ResourceManifest
		expect      string
		expectError bool
	}{
		"nothing": {
			given:  manifest.ResourceManifest{},
			expect: `{"metadata":{"name":""}}`,
		},
		"min-spec": {
			given: manifest.ResourceManifest{
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			expect: `{"metadata":{"name":""},"spec":{"value":1,"name":"life"}}`,
		},
		"basic": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("testSpec"),
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
			},
			expect: `{"kind":"testSpec","metadata":{"name":"test-spec"},"spec":{"value":42,"name":"meaning"}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := json.Marshal(test.given)
			require.NoError(t, err)
			require.Equal(t, test.expect, string(got))
		})
	}
}

func TestManifestUnmarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value        int                    `json:"value"`
		Name         string                 `json:"name"`
		Requirements manifest.LabelSelector `json:"requirements"`
	}

	testKind := manifest.Kind("testSpec")
	require.NoError(t, manifest.RegisterKind(testKind, &TestSpec{}))
	defer manifest.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       string
		expect      manifest.ResourceManifest
		expectError bool
	}{
		"nothing": {
			given:  `{"metadata":{"name":""}}`,
			expect: manifest.ResourceManifest{},
		},
		"unknown-kind": {
			given: `{"kind":"unknownSpec", "metadata":{"name":""},"spec":{"field":"xyz","desc":"unknown"}}`,
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("unknownSpec"),
				},
				Metadata: manifest.ObjectMeta{},
				Spec:     map[string]any{"field": "xyz", "desc": "unknown"},
			},
		},
		"min-spec": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			given: `{"kind":"testSpec", "metadata":{"name":""},"spec":{"value":1,"name":"life"}}`,
		},
		"basic": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
			},
			given: `{"kind":"testSpec","metadata":{"name":"test-spec"},"spec":{"value":42,"name":"meaning"}}`,
		},
		"spurious-status": {
			expectError: true,
			given:       `{"kind":"testSpec", "metadata":{"name":""},"spec":{"value":1,"name":"life"},"status":{"name":"daily","data":[1,2,3]}}`,
		},
		"basic_w_requirements": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec-1",
				},
				Spec: &TestSpec{
					Value: 42,
					Requirements: manifest.LabelSelector{
						MatchLabels: manifest.Labels{
							"cloud":    "ok",
							"labelled": "sure",
						},
					},
				},
			},
			given: `{"kind":"testSpec","metadata":{"name":"test-spec-1"},"spec":{"value":42,"requirements":{"matchLabels": {"cloud":"ok","labelled":"sure"}}}}`,
		},
		"basic_w_requirements_format_err": {
			expectError: true,
			given:       `{"kind":"testSpec","metadata":{"name":"test-spec-1"},"spec":{"value":42,"requirements":{"cloud":"ok","labelled":"sure"}}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var got manifest.ResourceManifest
			err := json.Unmarshal([]byte(test.given), &got)

			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}

func TestManifestMarshaling_YAML(t *testing.T) {
	type TestSpec struct {
		Value int    `yaml:"value"`
		Name  string `yaml:"name"`
	}
	type TestStatus struct {
		Name string `json:"name"`
		Data []int  `json:"data"`
	}

	testCases := map[string]struct {
		given       manifest.ResourceManifest
		expect      string
		expectError bool
	}{
		"nothing": {
			given: manifest.ResourceManifest{},
			expect: `metadata:
    name: ""
`,
		},
		"min-spec": {
			given: manifest.ResourceManifest{
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			expect: `metadata:
    name: ""
spec:
    value: 1
    name: life
`,
		},
		"basic": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("testSpec"),
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
			},
			expect: `kind: testSpec
metadata:
    name: test-spec
spec:
    value: 42
    name: meaning
`,
		},
		"basic-stateful": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("testSpec"),
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
				Status: &TestStatus{
					Name: "testStatus",
					Data: []int{4, 2, 1},
				},
			},
			expect: `kind: testSpec
metadata:
    name: test-spec
spec:
    value: 42
    name: meaning
status:
    name: testStatus
    data:
        - 4
        - 2
        - 1
`,
		},
		"state-only": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("testSpec"),
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Status: &TestStatus{
					Name: "test",
					Data: []int{3, 5, 0},
				},
			},
			expect: `kind: testSpec
metadata:
    name: test-spec
status:
    name: test
    data:
        - 3
        - 5
        - 0
`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := yaml.Marshal(test.given)
			require.NoError(t, err)
			require.Equal(t, test.expect, string(got))
		})
	}
}

func TestManifestUnmarshaling_YAML(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}

	testKind := manifest.Kind("testSpec")

	require.NoError(t, manifest.RegisterKind(testKind, &TestSpec{}))
	defer manifest.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       string
		expect      manifest.ResourceManifest
		expectError bool
	}{
		"nothing": {
			given: `metadata:
    name: "xyz"
`,
			expect: manifest.ResourceManifest{
				Metadata: manifest.ObjectMeta{
					Name: "xyz",
				},
			},
			expectError: !true,
		},
		"unknown-kind": {
			given: `kind: unknownSpec
metadata:
    name: ""
spec:
    value: 1
    name: life
`,
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("unknownSpec"),
				},
				Spec: map[string]any{
					"value": 1,
					"name":  "life",
				},
			},
			expectError: false,
		},
		"min-spec": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Spec: &TestSpec{},
			},
			given: `kind: testSpec
`,
		},
		"basic": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
			},
			given: `kind: testSpec
metadata:
    name: test-spec
spec:
    value: 42
    name: meaning
`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var got manifest.ResourceManifest
			err := yaml.Unmarshal([]byte(test.given), &got)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}

func TestStatefulManifestUnmarshaling_YAML(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}
	type TestStatus struct {
		Name string `json:"name"`
		Data []int  `json:"data"`
	}

	testKind := manifest.Kind("testSpec")

	require.NoError(t, manifest.RegisterManifest(testKind, &TestSpec{}, &TestStatus{}))
	defer manifest.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       string
		expect      manifest.ResourceManifest
		expectError bool
	}{
		"unknown-kind": {
			given: `kind: unknownSpec
metadata:
    name: ""
spec:
    value: 1
    name: life
status:
    name: what
    data:
         - 1    
`,
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("unknownSpec"),
				},
				Spec: map[string]any{
					"value": 1,
					"name":  "life",
				},
				Status: map[string]any{
					"name": "what",
					"data": []any{1},
				},
			},
		},
		"min-spec": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Spec:   &TestSpec{},
				Status: &TestStatus{},
			},
			given: `kind: testSpec
`,
		},
		"basic": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
				Status: &TestStatus{
					Name: "what",
					Data: []int{3, 5},
				},
			},
			given: `kind: testSpec
metadata:
    name: test-spec
spec:
    value: 42
    name: meaning
status:
    name: what
    data:
         - 3
         - 5
`,
		},
		"status-only": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{},
				Status: &TestStatus{
					Name: "what",
					Data: []int{3, 5},
				},
			},
			given: `kind: testSpec
metadata:
    name: test-spec
status:
    name: what
    data:
         - 3
         - 5
`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var got manifest.ResourceManifest
			err := yaml.Unmarshal([]byte(test.given), &got)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}

func TestCustomUnmarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `yaml:"value"`
		Name  string `yaml:"name"`
	}
	testKind := manifest.Kind("testSpec")

	require.NoError(t, manifest.RegisterKind(testKind, &TestSpec{}))
	defer manifest.UnregisterKind(testKind)

	testCases := map[string]struct {
		givenKind   manifest.Kind
		givenData   json.RawMessage
		expect      any
		expectError bool
	}{
		"unknown-kind-nil-data": {
			givenKind: "",
			givenData: nil,
			expect:    nil,
		},
		"unknown-kind-empty-data": {
			givenKind: "",
			givenData: json.RawMessage{},
			expect:    nil,
		},
		"known-kind-no-data": {
			givenKind: "testSpec",
			givenData: json.RawMessage{},
			expect:    nil,
		},
		"known-kind-with-data": {
			givenKind: "testSpec",
			givenData: json.RawMessage(`{"value":321,"name":"que"}`),
			expect: &TestSpec{
				Value: 321,
				Name:  "que",
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			gotManifest, err := manifest.UnmarshalJSONWithRegister(test.givenKind, manifest.InstanceOf, test.givenData, nil)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, gotManifest.Spec)
			}
		})
	}
}

func TestMetadataValidation(t *testing.T) {
	testCases := map[string]struct {
		given       manifest.ObjectMeta
		expectError bool
	}{
		"empty-value-ok": {given: manifest.ObjectMeta{}},
		"just.key": {
			given: manifest.ObjectMeta{
				Name: "name",
				Labels: manifest.Labels{
					"key":                 "",
					"app.k8s.io/key.name": "",
				},
			},
		},
		"invalid-labels": {
			given: manifest.ObjectMeta{
				Name: "name",
				Labels: manifest.Labels{
					"app.k8s.io/version":                "321",
					"app.k8s.io/version.1":              "+321",
					"app.k8s.io/version.semantic":       "1.2.3",
					"app.k8s.io/version.semantic.build": "1.2.3 dev",
				},
			},
			expectError: true,
		},
		"capital-name-invalid": {
			given: manifest.ObjectMeta{
				Name: "Name",
			},
			expectError: true,
		},
		"space-name-invalid": {
			given: manifest.ObjectMeta{
				Name: "name space",
			},
			expectError: true,
		},
		"numeric-names-ok": {
			given: manifest.ObjectMeta{
				Name: "9wha8",
			},
		},
		"names-cant-start-with-dash": {
			given: manifest.ObjectMeta{
				Name: "-wha8",
			},
			expectError: true,
		},
		"numeric-name-2-invalid": {
			given: manifest.ObjectMeta{
				Name: "9wha&8",
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

func TestStatefulManifestMarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}
	type TestStatus struct {
		Name string `json:"name"`
		Data []int  `json:"data"`
	}

	testCases := map[string]struct {
		given       manifest.ResourceManifest
		expect      string
		expectError bool
	}{
		"nothing": {
			given:  manifest.ResourceManifest{},
			expect: `{"metadata":{"name":""}}`,
		},
		"min-spec": {
			given: manifest.ResourceManifest{
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
				Status: &TestStatus{
					Name: "daily",
					Data: []int{1, 2, 3},
				},
			},
			expect: `{"metadata":{"name":""},"spec":{"value":1,"name":"life"},"status":{"name":"daily","data":[1,2,3]}}`,
		},
		"basic": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("testSpec"),
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
				Status: &TestStatus{
					Name: "daily",
					Data: []int{5, 7, 3},
				},
			},
			expect: `{"kind":"testSpec","metadata":{"name":"test-spec"},"spec":{"value":42,"name":"meaning"},"status":{"name":"daily","data":[5,7,3]}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := json.Marshal(test.given)
			require.NoError(t, err)
			require.Equal(t, test.expect, string(got))
		})
	}
}

func TestStatefulManifestUnmarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}
	type TestStatus struct {
		Name string `json:"name"`
		Data []int  `json:"data"`
	}

	testKind := manifest.Kind("testSpec")
	require.NoError(t, manifest.RegisterManifest(testKind, &TestSpec{}, &TestStatus{}))
	defer manifest.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       string
		expect      manifest.ResourceManifest
		expectError bool
	}{
		"nothing": {
			given:  `{"metadata":{"name":""}}`,
			expect: manifest.ResourceManifest{},
		},
		"unknown-kind": {
			given: `{"kind":"Unknown", "metadata":{"name":""},"spec":{"field":"xyz","desc":"unknown"},"status":{"value":"xyz","fields":"unknown"}}`,
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: manifest.Kind("Unknown"),
				},
				Metadata: manifest.ObjectMeta{},
				Spec:     map[string]any{"field": "xyz", "desc": "unknown"},
				Status:   map[string]any{"value": "xyz", "fields": "unknown"},
			},
		},
		"min-spec": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			given: `{"kind":"testSpec", "metadata":{"name":""},"spec":{"value":1,"name":"life"}}`,
		},
		"basic": {
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
				Status: &TestStatus{
					Name: "daily",
					Data: []int{5, 7, 3},
				},
			},
			given: `{"kind":"testSpec","metadata":{"name":"test-spec"},"spec":{"value":42,"name":"meaning"},"status":{"name":"daily","data":[5,7,3]}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var got manifest.ResourceManifest
			err := json.Unmarshal([]byte(test.given), &got)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}
