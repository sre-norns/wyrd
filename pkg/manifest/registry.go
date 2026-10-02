package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Registry resolves an exact (apiVersion, kind) pair. Each service owns its
// registry; importing a package cannot change another service's wire contract.
// The zero value is ready to use. Registration and lookup are concurrency safe.
// The older package-level kind registry remains available to existing callers.
type Registry struct {
	mu          sync.RWMutex
	definitions map[TypeMeta]resourceDefinition
}

type resourceDefinition struct {
	spec, status reflect.Type
	scopes       []Scope
}

// Register declares a resource's typed spec, optional status and allowed scopes.
// A kind may occur in several API groups. A duplicate exact pair is an error.
// A kind without explicit scopes is system scoped.
func (r *Registry) Register(meta TypeMeta, spec, status any, scopes ...Scope) error {
	group, version, ok := strings.Cut(meta.APIVersion, "/")
	if !ok || group == "" || version == "" || strings.Contains(version, "/") || meta.Kind == "" {
		return fmt.Errorf("invalid resource type: apiVersion and kind are required")
	}
	st, err := ExemplarType(spec)
	if err != nil {
		return err
	}
	if st == nil || st.Kind() != reflect.Struct {
		return fmt.Errorf("%w: spec must be a struct", ErrUnexpectedSpecType)
	}
	ot, err := ExemplarType(status)
	if err != nil {
		return err
	}
	if ot != nil && ot.Kind() != reflect.Struct {
		return fmt.Errorf("%w: status must be a struct", ErrStatusTypeInvalid)
	}
	if len(scopes) == 0 {
		scopes = []Scope{ScopeSystem}
	}
	seen := map[Scope]bool{}
	for _, scope := range scopes {
		if scope != ScopeSystem && scope != ScopeAccount && scope != ScopeProject {
			return fmt.Errorf("%w: %q", ErrInvalidScope, scope)
		}
		if seen[scope] {
			return fmt.Errorf("duplicate resource scope %q", scope)
		}
		seen[scope] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.definitions == nil {
		r.definitions = make(map[TypeMeta]resourceDefinition)
	}
	if _, exists := r.definitions[meta]; exists {
		return fmt.Errorf("resource %s/%s already registered", meta.APIVersion, meta.Kind)
	}
	r.definitions[meta] = resourceDefinition{spec: st, status: ot, scopes: append([]Scope(nil), scopes...)}
	return nil
}

func (r *Registry) lookup(meta TypeMeta) (resourceDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.definitions[meta]
	if !ok {
		return resourceDefinition{}, fmt.Errorf("%w: %s/%s", ErrUnknownKind, meta.APIVersion, meta.Kind)
	}
	return d, nil
}

// ValidateScope checks a resolved ownership reference against the kind's allowed
// scopes. It does not authorize the caller. Resolve ownership from the route and
// principal before calling it, and apply the product's authorization separately.
func (r *Registry) ValidateScope(meta TypeMeta, ref ScopeRef) error {
	d, err := r.lookup(meta)
	if err != nil {
		return err
	}
	for _, scope := range d.scopes {
		if ref.Validate(scope) == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: %s/%s does not permit this ownership", ErrInvalidScope, meta.APIVersion, meta.Kind)
}

// New creates an empty typed resource. It does not invent metadata or status.
func (r *Registry) New(meta TypeMeta) (ResourceManifest, error) {
	d, err := r.lookup(meta)
	if err != nil {
		return ResourceManifest{}, err
	}
	return ResourceManifest{TypeMeta: meta, Spec: reflect.New(d.spec).Interface()}, nil
}

// DecodeJSON reads a canonical resource, refusing unknown groups, kinds, fields,
// null specs and trailing input. Use a separate command/merge-patch decoder for
// writes that are not full resources. Decoding does not grant write authority.
func (r *Registry) DecodeJSON(data []byte) (ResourceManifest, error) {
	var wire struct {
		TypeMeta
		Metadata *ObjectMeta     `json:"metadata"`
		Spec     json.RawMessage `json:"spec"`
		Status   json.RawMessage `json:"status"`
		HResponse
	}
	if err := decodeJSON(data, &wire); err != nil {
		return ResourceManifest{}, err
	}
	d, err := r.lookup(wire.TypeMeta)
	if err != nil {
		return ResourceManifest{}, err
	}
	if wire.Metadata == nil || len(wire.Spec) == 0 || bytes.Equal(bytes.TrimSpace(wire.Spec), []byte("null")) {
		return ResourceManifest{}, fmt.Errorf("metadata and non-null spec are required")
	}
	out := ResourceManifest{TypeMeta: wire.TypeMeta, Metadata: *wire.Metadata, HResponse: wire.HResponse, Spec: reflect.New(d.spec).Interface()}
	if err := decodeJSON(wire.Spec, out.Spec); err != nil {
		return ResourceManifest{}, fmt.Errorf("spec: %w", err)
	}
	if len(wire.Status) > 0 {
		if d.status == nil || bytes.Equal(bytes.TrimSpace(wire.Status), []byte("null")) {
			return ResourceManifest{}, ErrStatusTypeInvalid
		}
		out.Status = reflect.New(d.status).Interface()
		if err := decodeJSON(wire.Status, out.Status); err != nil {
			return ResourceManifest{}, fmt.Errorf("status: %w", err)
		}
	}
	return out, nil
}

func decodeJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

// DecodeYAML is DecodeJSON's YAML counterpart. Typed fields retain their YAML
// decoding semantics (for example a duration), rather than passing through JSON.
func (r *Registry) DecodeYAML(data []byte) (ResourceManifest, error) {
	var wire struct {
		TypeMeta  `yaml:",inline"`
		Metadata  *ObjectMeta `yaml:"metadata"`
		Spec      yaml.Node   `yaml:"spec"`
		Status    yaml.Node   `yaml:"status"`
		HResponse `yaml:",inline"`
	}
	if err := decodeYAML(data, &wire); err != nil {
		return ResourceManifest{}, err
	}
	d, err := r.lookup(wire.TypeMeta)
	if err != nil {
		return ResourceManifest{}, err
	}
	if wire.Metadata == nil || wire.Spec.Kind != yaml.MappingNode {
		return ResourceManifest{}, fmt.Errorf("metadata and object spec are required")
	}
	out := ResourceManifest{TypeMeta: wire.TypeMeta, Metadata: *wire.Metadata, HResponse: wire.HResponse, Spec: reflect.New(d.spec).Interface()}
	spec, err := yaml.Marshal(&wire.Spec)
	if err != nil {
		return ResourceManifest{}, err
	}
	if err := decodeYAML(spec, out.Spec); err != nil {
		return ResourceManifest{}, fmt.Errorf("spec: %w", err)
	}
	if wire.Status.Kind != 0 {
		if d.status == nil || wire.Status.Kind != yaml.MappingNode {
			return ResourceManifest{}, ErrStatusTypeInvalid
		}
		out.Status = reflect.New(d.status).Interface()
		status, err := yaml.Marshal(&wire.Status)
		if err != nil {
			return ResourceManifest{}, err
		}
		if err := decodeYAML(status, out.Status); err != nil {
			return ResourceManifest{}, fmt.Errorf("status: %w", err)
		}
	}
	return out, nil
}

func decodeYAML(data []byte, value any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one YAML document")
	}
	return nil
}

// ValidateTypes refuses a response whose spec/status do not match its declared
// type. Authorization, field ownership, metadata and domain validation belong to
// the service. Call before encoding typed resources at the transport boundary.
func (r *Registry) ValidateTypes(value ResourceManifest) error {
	d, err := r.lookup(value.TypeMeta)
	if err != nil {
		return err
	}
	if !matchesType(value.Spec, d.spec) {
		return ErrSpecTypeInvalid
	}
	if value.Status != nil && !matchesType(value.Status, d.status) {
		return ErrStatusTypeInvalid
	}
	return nil
}

func matchesType(value any, want reflect.Type) bool {
	if value == nil || want == nil {
		return false
	}
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	return v.Type() == want
}
