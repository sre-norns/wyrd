package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

var (
	// ErrUnknownKind is the error returned when the 'kind' value goes not match any previously registered type.
	ErrUnknownKind = errors.New("unknown kind")
	// ErrUnexpectedSpecType is an error returned when type cast of a .spec in the manifest is not possible to the expected type.
	ErrUnexpectedSpecType = errors.New("unexpected spec type")
	// ErrUninterfacableType is an error returned when the type being registered can not be captured by interface.
	ErrUninterfacableType = errors.New("type can not interface")
)

// Kind represents ID of a type that can be used as a spec in a manifest
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
		return fmt.Errorf("kind %q already registered", kind)
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

// MustRegisterKind calls RegisterKind to registers a kind and panics on error
func MustRegisterKind(kind Kind, proto any) {
	if err := RegisterKind(kind, proto); err != nil {
		panic(err)
	}
}

// UnregisterKind unregisters previously registered 'kind' value
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

// KindOf returns `kind` id for the given type if its a registered kind.
// maybeSpec is the pointer to a spec value that you want to find corresponding [Kind] id of.
// result is the [Kind] id of the previously registered type.
// know is true if the maybeSpec is a value of previously registered type.
func KindOf(maybeSpec any) (result Kind, known bool) {
	val := reflect.ValueOf(maybeSpec)
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

// MustKnowKindOf returns [Kind] id of a type or panics if the type has not been previously registered.
func MustKnowKindOf(maybeSpec any) (kind Kind) {
	kind, ok := KindOf(maybeSpec)
	if !ok {
		panic(ErrUnknownKind)
	}

	return
}

// TypeMeta describe common API information for each API object.
type TypeMeta struct {
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       Kind   `json:"kind,omitempty" yaml:"kind,omitempty" binding:"required"`
}

// ObjectMeta represents common information about resources managed by a service.
type ObjectMeta struct {
	// System generated unique identified of this object
	UID ResourceID `form:"uid,omitempty" json:"uid,omitempty" yaml:"uid,omitempty" gorm:"primaryKey;not null;type:uuid"`

	// A sequence number representing a specific generation of the resource.
	// Populated by the system. Read-only.
	Version Version `form:"version,omitempty" json:"version,omitempty" yaml:"version,omitempty" xml:"version,omitempty" gorm:"default:1"`

	// Name is a unique human-readable identifier of a resource
	Name string `form:"name,omitempty" json:"name" yaml:"name" gorm:"uniqueIndex;not null;"`
	// TODO: Set null on delete? gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;

	// Labels is map of string keys and values that can be used to organize and categorize
	// (scope and select) resources.
	Labels Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" gorm:"serializer:json"`

	// CreatedAt is time when the object was created on the server.
	// It is populated by the system and clients may not set this value.
	// Read-only.
	CreatedAt *time.Time `form:"creationTimestamp,omitempty" json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty" xml:"creationTimestamp,omitempty"`
	// UpdatedAt is time when the object was last update on the server.
	// It is populated by the system and clients may not set this value.
	// Read-only.
	UpdatedAt *time.Time `form:"updateTimestamp,omitempty" json:"updateTimestamp,omitempty" yaml:"updateTimestamp,omitempty" xml:"updateTimestamp,omitempty"`
	// DeletedAt is time when the object was deleted on the server if ever.
	// This time is recorded to implement 'tombstones' - objects content may be deleted, while the record of its deletion is retained.
	// It is populated by the system and clients may not set this value.
	// Read-only.
	DeletedAt *time.Time `form:"deletionTimestamp,omitempty" json:"deletionTimestamp,omitempty" yaml:"deletionTimestamp,omitempty" xml:"deletionTimestamp,omitempty" gorm:"index"`
}

func (m *ObjectMeta) BeforeCreate(tx *gorm.DB) (err error) {
	if m.UID == "" {
		m.UID = ResourceID(uuid.NewString())
	}
	return
}

func (m *ObjectMeta) BeforeSave(tx *gorm.DB) (err error) {
	m.Version += 1
	return
}

func (m ObjectMeta) GetVersionedID() VersionedResourceID {
	return VersionedResourceID{
		ID:      m.UID,
		Version: m.Version,
	}
}

// ResourceManifest is a Custom Resource Definition.
type ResourceManifest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     any        `json:"-" yaml:"-"`

	// Links is not a part of CRD spec, but part of semantic model, it defines actions applicable to this model
	HResponse `json:",inline" yaml:",inline"`
}

// MarshalJSON is an implementation of golang [encoding/json.Marshaler] interface
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

	decoder := json.NewDecoder(bytes.NewReader(specData))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(spec)

	return spec, err
}

// UnmarshalJSON is an implementation of golang [encoding/json.Unmarshaler] interface
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

// MarshalYAML returns a value that can be easily marshaled to yaml representation.
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

// UnmarshalYAML decodes manifest object from YAML representation.
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
