package manifest_test

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type registrySpec struct {
	Description string            `json:"description" yaml:"description"`
	Timeout     time.Duration     `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Values      map[string]string `json:"values,omitempty" yaml:"values,omitempty"`
}
type registryStatus struct {
	Phase string `json:"phase" yaml:"phase"`
}

func resourceRegistry(t *testing.T) (*manifest.Registry, manifest.TypeMeta) {
	t.Helper()
	r := new(manifest.Registry)
	meta := manifest.TypeMeta{APIVersion: "identity.sre-norns.com/v1", Kind: "projects"}
	require.NoError(t, r.Register(meta, registrySpec{}, registryStatus{}, manifest.ScopeAccount))
	return r, meta
}

func TestRegistryGroupIsolationAndScope(t *testing.T) {
	r, meta := resourceRegistry(t)
	other := manifest.TypeMeta{APIVersion: "exp-bench.sre-norns.com/v1", Kind: meta.Kind}
	require.NoError(t, r.Register(other, struct {
		Target string `json:"target"`
	}{}, nil, manifest.ScopeSystem, manifest.ScopeProject))
	require.Error(t, r.Register(meta, registrySpec{}, nil))
	require.NoError(t, r.ValidateScope(meta, manifest.ScopeRef{Account: "a"}))
	require.ErrorIs(t, r.ValidateScope(meta, manifest.ScopeRef{Account: "a", Project: "p"}), manifest.ErrInvalidScope)
	require.NoError(t, r.ValidateScope(other, manifest.ScopeRef{}))
	require.NoError(t, r.ValidateScope(other, manifest.ScopeRef{Account: "a", Project: "p"}))
	require.ErrorIs(t, r.ValidateScope(other, manifest.ScopeRef{Project: "p"}), manifest.ErrInvalidScope)
	value, err := r.New(meta)
	require.NoError(t, err)
	require.IsType(t, &registrySpec{}, value.Spec)
	require.Nil(t, value.Status)
	require.NoError(t, r.ValidateTypes(value))
	value.TypeMeta = other
	require.ErrorIs(t, r.ValidateTypes(value), manifest.ErrSpecTypeInvalid)
	value.TypeMeta = meta
	value.Status = &struct{}{}
	require.ErrorIs(t, r.ValidateTypes(value), manifest.ErrStatusTypeInvalid)
	value.Spec = (*registrySpec)(nil)
	require.ErrorIs(t, r.ValidateTypes(value), manifest.ErrSpecTypeInvalid)
	require.Error(t, r.Register(manifest.TypeMeta{Kind: "x"}, registrySpec{}, nil))
	require.Error(t, r.Register(other, []string{}, nil))
	require.Error(t, r.Register(other, registrySpec{}, "invalid"))
	require.Error(t, r.Register(other, registrySpec{}, nil, "invalid"))
}

func TestRegistryJSONAndYAML(t *testing.T) {
	r, meta := resourceRegistry(t)
	original := manifest.ResourceManifest{
		TypeMeta:  meta,
		Metadata:  manifest.ObjectMeta{UID: "p", Name: "checkout", Account: "a", Version: 7, Labels: manifest.Labels{"user_key": "same"}},
		Spec:      &registrySpec{Description: "Checkout", Timeout: 3 * time.Second, Values: map[string]string{"user_key": "same"}},
		Status:    &registryStatus{Phase: "active"},
		HResponse: manifest.HResponse{Links: map[string]manifest.HLink{"self": {Reference: "/v1/projects/p", Relationship: "self"}}},
	}
	encoded, err := json.Marshal(original)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"_links"`)
	decoded, err := r.DecodeJSON(encoded)
	require.NoError(t, err)
	require.Equal(t, original, decoded)
	yamlBytes, err := yaml.Marshal(original)
	require.NoError(t, err)
	require.Contains(t, string(yamlBytes), "timeout: 3s")
	require.Contains(t, string(yamlBytes), "ref: /v1/projects/p")
	decoded, err = r.DecodeYAML(yamlBytes)
	require.NoError(t, err)
	require.Equal(t, original, decoded)
	// The existing generic reader also retains semantic links.
	var generic manifest.ResourceManifest
	require.NoError(t, json.Unmarshal(encoded, &generic))
	require.Equal(t, original.HResponse, generic.HResponse)
}

func TestRegistryRejectsUnsupportedResources(t *testing.T) {
	r, _ := resourceRegistry(t)
	valid := `{"apiVersion":"identity.sre-norns.com/v1","kind":"projects","metadata":{},"spec":{"description":"ok"}}`
	for name, data := range map[string]string{
		"flat":               `{"id":"p","description":"ok"}`,
		"unknown group":      strings.Replace(valid, "identity.sre-norns.com", "unknown.sre-norns.com", 1),
		"unknown kind":       strings.Replace(valid, `"projects"`, `"secrets"`, 1),
		"unknown spec field": strings.Replace(valid, `"description"`, `"password"`, 1),
		"unknown metadata":   strings.Replace(valid, `"metadata":{}`, `"metadata":{"password":"secret"}`, 1),
		"mixed flat":         strings.Replace(valid, `"metadata":{}`, `"id":"p","metadata":{}`, 1),
		"null spec":          strings.Replace(valid, `{"description":"ok"}`, `null`, 1),
		"null metadata":      strings.Replace(valid, `"metadata":{}`, `"metadata":null`, 1),
		"trailing input":     valid + ` {}`,
		"null resource":      `null`,
	} {
		t.Run(name, func(t *testing.T) { _, err := r.DecodeJSON([]byte(data)); require.Error(t, err) })
	}
	validYAML := "apiVersion: identity.sre-norns.com/v1\nkind: projects\nmetadata: {}\nspec:\n  description: ok\n"
	for _, data := range []string{validYAML + "unknown: true\n", strings.Replace(validYAML, "description:", "password:", 1), validYAML + "---\n{}\n", validYAML + "status: null\n"} {
		_, err := r.DecodeYAML([]byte(data))
		require.Error(t, err)
	}
}

func TestRegistryConcurrentReads(t *testing.T) {
	r, meta := resourceRegistry(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 20 {
				_, err := r.New(meta)
				if err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}

func TestM8ProjectExamples(t *testing.T) {
	type projectSpec struct {
		Description string `json:"description" yaml:"description"`
		Target      string `json:"target" yaml:"target"`
	}
	type projectStatus struct {
		Phase string `json:"phase" yaml:"phase"`
	}
	r := new(manifest.Registry)
	require.NoError(t, r.Register(manifest.TypeMeta{APIVersion: "identity.sre-norns.com/v1", Kind: "projects"}, projectSpec{}, projectStatus{}, manifest.ScopeAccount))
	for _, format := range []string{"json", "yaml"} {
		data, err := os.ReadFile("../../docs/examples/m8/project." + format)
		require.NoError(t, err)
		var v manifest.ResourceManifest
		if format == "json" {
			v, err = r.DecodeJSON(data)
		} else {
			v, err = r.DecodeYAML(data)
		}
		require.NoError(t, err)
		require.NoError(t, r.ValidateTypes(v))
		require.NoError(t, r.ValidateScope(v.TypeMeta, v.Metadata.Scope()))
		require.Equal(t, manifest.Version(1), v.Metadata.Version)
	}
	create, err := os.ReadFile("../../docs/examples/m8/project-create.json")
	require.NoError(t, err)
	request, err := r.DecodeJSON(create)
	require.NoError(t, err)
	require.Empty(t, request.Metadata.UID)
	require.Empty(t, request.Metadata.Account)
	require.Zero(t, request.Metadata.Version)
	require.Nil(t, request.Status)

}
