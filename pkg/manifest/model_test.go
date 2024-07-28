package manifest_test

import (
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

type TestSpec struct {
	Value int    `json:"value"`
	Name  string `json:"name"`
}
type TestStatus struct {
	Name string `json:"name"`
	Data []int  `json:"data"`
}

type TestStatelessModel manifest.ResourceModel[TestSpec]
type TestStatefulModel manifest.StatefulResource[TestSpec, TestStatus]

func TestStatelessManifestToModelCasting(t *testing.T) {
	testKind := manifest.Kind("testSpec")
	require.NoError(t, manifest.RegisterKind(testKind, &TestSpec{}))
	defer manifest.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       manifest.ResourceManifest
		expect      TestStatelessModel
		expectError bool
	}{
		"unknown-kind": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: "new",
				},
				Spec: &TestSpec{
					Name: "test-spec",
				},
			},
			expectError: true,
		},
		"nil-spec": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Spec: nil,
			},
			expectError: true,
		},
		"basic-spec": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Spec: &TestSpec{
					Name:  "test-spec",
					Value: 3,
				},
			},
			expect: TestStatelessModel{
				ObjectMeta: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Spec: TestSpec{
					Name:  "test-spec",
					Value: 3,
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := manifest.ManifestAsResource[TestSpec](test.given)

			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, TestStatelessModel(got))
			}
		})
	}
}

func TestStatefulManifestToModelCasting(t *testing.T) {
	testKind := manifest.Kind("StatefulManifest")
	require.NoError(t, manifest.RegisterManifest(testKind, &TestSpec{}, &TestStatus{}))
	defer manifest.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       manifest.ResourceManifest
		expect      TestStatelessModel
		expectError bool
	}{
		"unknown-kind": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: "new",
				},
				Spec: &TestSpec{
					Name: "test-spec",
				},
			},
			expectError: true,
		},
		"nil-spec": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Spec: nil,
			},
			expectError: true,
		},
		"nil-status": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Spec: &TestSpec{
					Name:  "test-spec",
					Value: 3,
				},
				Status: nil,
			},
			expect: TestStatelessModel{
				ObjectMeta: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Spec: TestSpec{
					Name:  "test-spec",
					Value: 3,
				},
			},
		},
		"basic-all": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Spec: &TestSpec{
					Name:  "test-spec",
					Value: 3,
				},
			},
			expect: TestStatelessModel{
				ObjectMeta: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Spec: TestSpec{
					Name:  "test-spec",
					Value: 3,
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := manifest.ManifestAsResource[TestSpec](test.given)

			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, TestStatelessModel(got))
			}
		})
	}
}

func TestManifestWithStatusOnlyToModelCasting(t *testing.T) {
	testKind := manifest.Kind("StatefulManifest")
	require.NoError(t, manifest.RegisterManifest(testKind, nil, &TestStatus{}))
	defer manifest.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       manifest.ResourceManifest
		expect      TestStatefulModel
		expectError bool
	}{
		"unknown-kind": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: "new",
				},
				Spec: &TestSpec{
					Name: "test-spec",
				},
			},
			expectError: true,
		},
		"nil-spec-n-status": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Spec:   nil,
				Status: nil,
			},
			expectError: true,
		},
		"nil-status": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Spec: &TestSpec{
					Name:  "test-spec",
					Value: 3,
				},
				Status: nil,
			},
			expectError: true,
		},
		"basic-all": {
			given: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: testKind,
				},
				Metadata: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Status: &TestStatus{
					Name: "test-spec",
					Data: []int{3, 8, 4},
				},
			},
			expect: TestStatefulModel{
				ObjectMeta: manifest.ObjectMeta{
					Name: "test-basic",
				},
				Status: TestStatus{
					Name: "test-spec",
					Data: []int{3, 8, 4},
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := manifest.ManifestAsStatefulResource[TestSpec, TestStatus](test.given)

			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, TestStatefulModel(got))
			}
		})
	}
}
