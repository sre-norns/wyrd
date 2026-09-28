package manifest_test

import (
	"encoding/json"
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestScopeRefValidate(t *testing.T) {
	account := manifest.ScopeRef{Account: "a"}
	project := manifest.ScopeRef{Account: "a", Project: "p"}
	orphan := manifest.ScopeRef{Project: "p"}

	cases := []struct {
		scope manifest.Scope
		ref   manifest.ScopeRef
		ok    bool
	}{
		{manifest.ScopeSystem, manifest.ScopeRef{}, true},
		{"", manifest.ScopeRef{}, true},
		{manifest.ScopeSystem, account, false},
		{manifest.ScopeAccount, account, true},
		{manifest.ScopeAccount, project, false},
		{manifest.ScopeAccount, manifest.ScopeRef{}, false},
		{manifest.ScopeProject, project, true},
		{manifest.ScopeProject, account, false},
		{manifest.ScopeProject, orphan, false},
		{manifest.Scope("galaxy"), manifest.ScopeRef{}, false},
	}
	for _, c := range cases {
		err := c.ref.Validate(c.scope)
		if c.ok {
			require.NoError(t, err, "%q %+v", c.scope, c.ref)
		} else {
			require.ErrorIs(t, err, manifest.ErrInvalidScope, "%q %+v", c.scope, c.ref)
		}
	}
}

func TestApplyScope(t *testing.T) {
	ref := manifest.ScopeRef{Account: "a", Project: "p"}

	var empty manifest.ObjectMeta
	require.NoError(t, empty.ApplyScope(ref))
	require.Equal(t, ref, empty.Scope())

	same := manifest.ObjectMeta{Account: "a", Project: "p"}
	require.NoError(t, same.ApplyScope(ref))

	// A body naming another scope is refused, and left as it was.
	other := manifest.ObjectMeta{Account: "a", Project: "q"}
	require.ErrorIs(t, other.ApplyScope(ref), manifest.ErrScopeMismatch)
	require.Equal(t, manifest.ResourceID("q"), other.Project)

	elsewhere := manifest.ObjectMeta{Account: "b"}
	require.ErrorIs(t, elsewhere.ApplyScope(ref), manifest.ErrScopeMismatch)
}

// Unscoped resources must serialise exactly as they did before scope existed.
func TestScopeIsOmittedWhenEmpty(t *testing.T) {
	data, err := json.Marshal(manifest.ObjectMeta{Name: "x"})
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"x"}`, string(data))

	data, err = json.Marshal(manifest.ObjectMeta{Name: "x", Account: "a", Project: "p"})
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"x","account":"a","project":"p"}`, string(data))
}

type scopedSpec struct{ Value int }
type unscopedSpec struct{ Value int }

func TestRegisterKindWithScope(t *testing.T) {
	const scoped, unscoped, bogus = manifest.Kind("scope-test-scoped"), manifest.Kind("scope-test-unscoped"), manifest.Kind("scope-test-bogus")
	t.Cleanup(func() {
		manifest.UnregisterKind(scoped)
		manifest.UnregisterKind(unscoped)
		manifest.UnregisterKind(bogus)
	})

	require.NoError(t, manifest.RegisterManifest(scoped, &scopedSpec{}, nil, manifest.WithScope(manifest.ScopeProject)))
	require.NoError(t, manifest.RegisterKind(unscoped, &unscopedSpec{}))

	scope, known := manifest.ScopeOf(scoped)
	require.True(t, known)
	require.Equal(t, manifest.ScopeProject, scope)

	scope, known = manifest.ScopeOf(unscoped)
	require.True(t, known)
	require.Equal(t, manifest.ScopeSystem, scope, "a kind registered without a scope keeps today's meaning")

	_, known = manifest.ScopeOf("scope-test-never-registered")
	require.False(t, known)

	require.ErrorIs(t, manifest.RegisterKind(bogus, &scopedSpec{}, manifest.WithScope("galaxy")), manifest.ErrInvalidScope)
	_, known = manifest.ScopeOf(bogus)
	require.False(t, known, "a refused registration must not be recorded")
}
