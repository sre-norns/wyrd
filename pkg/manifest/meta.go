package manifest

import (
	"encoding/json"
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

var (
	ErrUnknownKind        = fmt.Errorf("unknown kind")
	ErrUnexpectedSpecType = fmt.Errorf("unexpected spec type")
	ErrUninterfacableType = fmt.Errorf("type can not interface")
)

// Kind type to represent ID of a type that can be used as a spec in a manifest
type Kind string

// Registry of types that can be used in a manifest spec
var metaKindRegistry = map[Kind]reflect.Type{}

// RegisterKind is called to associate given 'kind' ID with a given type.
// Later, an instance of that 'kind' can be created using `InstanceOf`
// Usage:
// ```
// obj, err := wyrd.RegisterKind(wyrd.Kind("mySpec"), &MySpec{})
// ```
// Note: it is an error to double register the same `kind`.
func RegisterKind(kind Kind, proto any) error {
	if _, know := metaKindRegistry[kind]; know {
		return fmt.Errorf("Kind %q already registered", kind)
	}

	val := reflect.ValueOf(proto)
	if !val.CanInterface() {
		return fmt.Errorf("%q %w", val.Type(), ErrUninterfacableType)
	}

	t := val.Type()
	if val.Kind() == reflect.Pointer {
		t = val.Elem().Type()
	}

	metaKindRegistry[kind] = t
	return nil
}

func UnregisterKind(kind Kind) {
	delete(metaKindRegistry, kind)
}

// KindFactory is a type of function that creates instances of a given `Kind`
type KindFactory func(kind Kind) (any, error)

// InstanceOf is a default `KindFactory` to create instances of previously registered kinds
func InstanceOf(kind Kind) (any, error) {
	t, known := metaKindRegistry[kind]
	if !known {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}

	return reflect.New(t).Interface(), nil
}

// KindOf looks-up `kind` id for the given type.
func KindOf(maybeManifest any) (result Kind, known bool) {
	val := reflect.ValueOf(maybeManifest)
	t := val.Type()
	if val.Kind() == reflect.Pointer {
		t = val.Elem().Type()
	}

	// Linear scan over map to find key with value equals give: not that terrible when the map is small
	for kind, v := range metaKindRegistry {
		if v == t {
			return kind, true
		}
	}

	return
}

// MustKnowKindOf returns `kind` id of a type and panics if the type is not registered.
func MustKnowKindOf(maybeManifest any) (kind Kind) {
	kind, ok := KindOf(maybeManifest)
	if !ok {
		panic(ErrUnknownKind)
	}

	return
}

// TypeMeta describe individual objects returned by API
type TypeMeta struct {
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       Kind   `json:"kind,omitempty" yaml:"kind,omitempty" binding:"required"`
}

// ObjectMeta represents common information about objects in a systems.
type ObjectMeta struct {
	// System generated unique identified of this object
	UUID ResourceID `json:"uid,omitempty" yaml:"uid,omitempty"`

	// A sequence number representing a specific generation of the resource.
	// Populated by the system. Read-only.
	Version Version `form:"version,omitempty" json:"version,omitempty" yaml:"version,omitempty" xml:"version,omitempty" gorm:"default:1"`

	// Name is a unique human-readable identifier of a resource
	Name string `json:"name" yaml:"name" binding:"required" gorm:"uniqueIndex"`

	// Labels is map of string keys and values that can be used to organize and categorize
	// (scope and select) resources.
	Labels Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
}

// ResourceManifest is an implementation of custom resource definition.
type ResourceManifest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     any        `json:"-" yaml:"-"`
}

// MarshalJSON is an implementation of golang [Marshaler](https://pkg.go.dev/encoding/json#Marshaler) interface
func (s ResourceManifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		TypeMeta `json:",inline"`
		Metadata ObjectMeta `json:"metadata"`
		Spec     any        `json:"spec,omitempty"` // needed to strip any json tags
	}{
		TypeMeta: s.TypeMeta,
		Metadata: s.Metadata,
		Spec:     s.Spec,
	})
}

// UnmarshalJSONWithRegister is a "helper" method to unmarshal expected `kind` spec using given factory and RawJson data.
func UnmarshalJSONWithRegister(kind Kind, factory KindFactory, specData json.RawMessage) (any, error) {
	spec, err := factory(kind)
	if err != nil { // Kind is not known, get raw message if not-nil
		if len(specData) != 0 { // Is there a spec to parse
			t := make(map[string]any)
			if err := json.Unmarshal(specData, &t); err == nil {
				spec = t
			} else {
				spec = specData
			}
		}
		return spec, nil
	}

	if len(specData) == 0 { // No spec to parse
		return nil, nil
	}

	err = json.Unmarshal(specData, spec)
	return spec, err
}

// UnmarshalJSON is an implementation of golang [Unmarshaler](https://pkg.go.dev/encoding/json#Unmarshaler) interface
func (s *ResourceManifest) UnmarshalJSON(data []byte) (err error) {
	aux := struct {
		TypeMeta `json:",inline"`
		Metadata ObjectMeta `json:"metadata"`
		Spec     json.RawMessage
	}{
		TypeMeta: s.TypeMeta,
		Metadata: s.Metadata,
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	s.TypeMeta = aux.TypeMeta
	s.Metadata = aux.Metadata
	s.Spec, err = UnmarshalJSONWithRegister(aux.Kind, InstanceOf, aux.Spec)
	return
}

func (s ResourceManifest) MarshalYAML() (interface{}, error) {
	return struct {
		TypeMeta `json:",inline" yaml:",inline"`
		Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
		Spec     any        `json:"spec" yaml:"spec,omitempty"` // needed to strip any json tags
	}{
		TypeMeta: s.TypeMeta,
		Metadata: s.Metadata,
		Spec:     s.Spec,
	}, nil
}

func (s *ResourceManifest) UnmarshalYAML(n *yaml.Node) (err error) {
	type S ResourceManifest
	type T struct {
		*S   `yaml:",inline"`
		Spec yaml.Node `yaml:"spec"`
	}

	obj := &T{S: (*S)(s)}
	if err = n.Decode(obj); err != nil {
		return
	}

	s.Spec, err = InstanceOf(s.Kind)
	if err != nil {
		if len(obj.Spec.Content) == 0 {
			s.Spec = nil
			return nil
		}
		s.Spec = make(map[string]any)
	}

	return obj.Spec.Decode(s.Spec)
}
