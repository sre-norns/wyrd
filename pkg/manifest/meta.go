package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// Registry of types that can be used in a manifest spec
var metaKindRegistry = map[Kind]KindSpec{}

func ExemplarType(spec any) (reflect.Type, error) {
	if spec == nil {
		return nil, nil
	}

	val := reflect.ValueOf(spec)
	if !val.CanInterface() {
		return nil, fmt.Errorf("%q %w", val.Type(), ErrUninterfacableType)
	}

	t := val.Type()
	if val.Kind() == reflect.Pointer {
		t = val.Elem().Type()
	}

	return t, nil
}

// RegisterKind is called to associate given 'kind' ID with a given type.
// Later, an instance of that 'kind' can be created using `InstanceOf`
// Usage:
// ```
// obj, err := manifest.RegisterKind(manifest.Kind("mySpec"), &MySpec{})
// ```
// Note: it is an error to double register the same `kind`.
func RegisterKind(kind Kind, spec any) error {
	return RegisterManifest(kind, spec, nil)
}

func RegisterManifest(kind Kind, spec, status any) error {
	if _, know := metaKindRegistry[kind]; know {
		return fmt.Errorf("kind %q already registered", kind)
	}

	specType, err := ExemplarType(spec)
	if err != nil {
		return err
	}
	statusType, err := ExemplarType(status)
	if err != nil {
		return err
	}

	metaKindRegistry[kind] = KindSpec{
		SpecType:   specType,
		StatusType: statusType,
	}

	return nil
}

// MustRegisterKind calls RegisterKind to registers a kind and panics on error.
func MustRegisterKind(kind Kind, proto any) {
	if err := RegisterKind(kind, proto); err != nil {
		panic(err)
	}
}

// MustRegisterManifest registers types for a stateful manifest and panics on error.
func MustRegisterManifest(kind Kind, specType, statusType any) {
	if err := RegisterManifest(kind, specType, statusType); err != nil {
		panic(err)
	}
}

// UnregisterKind unregisters previously registered 'kind' value
func UnregisterKind(kind Kind) {
	delete(metaKindRegistry, kind)
}

func LookupKind(kind Kind) (result KindSpec, known bool) {
	result, known = metaKindRegistry[kind]
	return
}

// KindFactory is a type of function that creates instances of a given `Kind`
type KindFactory func(kind Kind) (ResourceManifest, error)

// InstanceOf is a default `KindFactory` to create instances of previously registered kinds
func InstanceOf(kind Kind) (ResourceManifest, error) {
	kindSpec, known := metaKindRegistry[kind]
	if !known {
		return ResourceManifest{}, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}

	result := ResourceManifest{
		TypeMeta: TypeMeta{
			Kind: kind,
		},
	}

	if kindSpec.SpecType != nil {
		result.Spec = reflect.New(kindSpec.SpecType).Interface()
	}
	if kindSpec.StatusType != nil {
		result.Status = reflect.New(kindSpec.StatusType).Interface()
	}

	return result, nil
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
	for kind, kindSpec := range metaKindRegistry {
		if kindSpec.SpecType == t {
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
	// UID is a system generated unique identified of this instance of an object.
	// see https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#uids
	UID ResourceID `form:"uid,omitempty" json:"uid,omitempty" yaml:"uid,omitempty" gorm:"primaryKey;not null;type:uuid"`

	// Version is a sequence number representing a specific generation of the resource.
	// Populated by the system. Read-only.
	Version Version `form:"version,omitempty" json:"version,omitempty" yaml:"version,omitempty" xml:"version,omitempty" gorm:"default:1"`

	// Name is a unique identifier of a resource provided by the resource owner.
	// see: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
	Name ResourceName `form:"name,omitempty" json:"name" yaml:"name" gorm:"index:idx_name;index:,unique,composite:deleted_name;not null"`

	// Labels is map of string keys and values that can be used to organize and categorize
	// (scope and select) resources.
	Labels Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" gorm:"serializer:json;type:json"`

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
	DeletedAt *gorm.DeletedAt `form:"deletionTimestamp,omitempty" json:"deletionTimestamp,omitempty" yaml:"deletionTimestamp,omitempty" xml:"deletionTimestamp,omitempty" gorm:"index:,composite:deleted_name_name,option:NULLS NOT DISTINCT"`
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

func (m ObjectMeta) Validate() error {
	errs := ErrorSet{}

	if m.Name != "" {
		if err := m.Name.ValidateSubdomainName(); err != nil {
			errs = append(errs, err)
		}
	} // TODO: Should we allow empty names?

	if err := m.Labels.Validate(); err != nil {
		if errSet, ok := err.(ErrorSet); ok {
			errs = append(errs, errSet...)
		} else {
			errs = append(errs, err)
		}
	}

	return errs.ErrorOrNil()
}

// ResourceManifest is a Custom Resource Definition.
type ResourceManifest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     any        `json:"-" yaml:"-"`
	Status   any        `json:"-" yaml:"-"`

	// Links is not a part of CRD spec, but part of semantic model, it defines actions applicable to this model
	HResponse `json:",inline" yaml:",inline"`
}

// MarshalJSON is an implementation of golang [encoding/json.Marshaler] interface
func (s ResourceManifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		TypeMeta `json:",inline"`
		Metadata ObjectMeta `json:"metadata"`
		Spec     any        `json:"spec,omitempty"`   // needed to strip any json tags
		Status   any        `json:"status,omitempty"` // needed to strip any json tags
	}{
		TypeMeta: s.TypeMeta,
		Metadata: s.Metadata,
		Spec:     s.Spec,
		Status:   s.Status,
	})
}

func tryPreserveJSON(data json.RawMessage) any {
	if len(data) != 0 { // Is there a spec to parse
		t := make(map[string]any)
		if err := json.Unmarshal(data, &t); err == nil {
			return t
		} else {
			return data
		}
	}

	return nil
}

// UnmarshalJSONWithRegister is a "helper" method to unmarshal expected `kind` spec using given factory and RawJson data.
func UnmarshalJSONWithRegister(kind Kind, factory KindFactory, specData json.RawMessage, statusData json.RawMessage) (ResourceManifest, error) {
	resource, err := factory(kind)
	if err != nil { // Kind is not known, get raw message if not-nil
		resource.Spec = tryPreserveJSON(specData)
		resource.Status = tryPreserveJSON(statusData)
		return resource, nil
	}

	if len(specData) != 0 {
		if resource.Spec == nil {
			return resource, fmt.Errorf("manifest has no spec type associated")
		}
		decoder := json.NewDecoder(bytes.NewReader(specData))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(resource.Spec); err != nil {
			return resource, fmt.Errorf("failed to decode spec: %w", err)
		}
	} else { // No spec to parse
		resource.Spec = nil
	}

	if len(statusData) != 0 {
		if resource.Status == nil {
			return resource, fmt.Errorf("manifest has no status type associated")
		}

		decoder := json.NewDecoder(bytes.NewReader(statusData))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(resource.Status); err != nil {
			return resource, fmt.Errorf("failed to decode status: %w", err)
		}
	} else { // No spec to parse
		resource.Status = nil
	}

	return resource, err
}

// UnmarshalJSON is an implementation of golang [encoding/json.Unmarshaler] interface
func (s *ResourceManifest) UnmarshalJSON(data []byte) (err error) {
	aux := struct {
		TypeMeta `json:",inline"`
		Metadata ObjectMeta      `json:"metadata"`
		Spec     json.RawMessage `json:"spec"`
		Status   json.RawMessage `json:"status"`
	}{
		TypeMeta: s.TypeMeta,
		Metadata: s.Metadata,
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*s, err = UnmarshalJSONWithRegister(aux.Kind, InstanceOf, aux.Spec, aux.Status)
	s.TypeMeta = aux.TypeMeta
	s.Metadata = aux.Metadata
	return
}

// MarshalYAML returns a value that can be easily marshaled to yaml representation.
func (s ResourceManifest) MarshalYAML() (interface{}, error) {
	return struct {
		TypeMeta `json:",inline" yaml:",inline"`
		Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
		Spec     any        `json:"spec" yaml:"spec,omitempty"`     // needed to strip any json tags
		Status   any        `json:"status" yaml:"status,omitempty"` // needed to strip any json tags
	}{
		TypeMeta: s.TypeMeta,
		Metadata: s.Metadata,
		Spec:     s.Spec,
		Status:   s.Status,
	}, nil
}

// UnmarshalYAML decodes manifest object from YAML representation.
func (s *ResourceManifest) UnmarshalYAML(n *yaml.Node) (err error) {
	type S ResourceManifest
	// type T struct {
	// 	*S   `yaml:",inline"`
	// 	Spec yaml.Node `yaml:"spec"`
	// }
	// obj := &T{S: (*S)(s)}

	obj := &struct {
		*S     `yaml:",inline"`
		Spec   yaml.Node `yaml:"spec"`
		Status yaml.Node `yaml:"status"`
	}{
		S: (*S)(s),
	}

	if err = n.Decode(obj); err != nil {
		return
	}

	// result.TypeMeta = obj.TypeMeta
	// result.Metadata = obj.Metadata

	result, err := InstanceOf(s.Kind)
	if err != nil {
		if len(obj.Spec.Content) == 0 {
			result.Spec = nil
		} else {
			result.Spec = make(map[string]any)
		}
		if len(obj.Status.Content) == 0 {
			result.Status = nil
		} else {
			result.Status = make(map[string]any)
		}

		if result.Spec == nil && result.Status == nil {
			// s.Spec = nil
			// s.Status = nil
			return nil
		}
	}

	if result.Spec != nil {
		if err := obj.Spec.Decode(result.Spec); err != nil {
			return fmt.Errorf("failed to decode spec from YML: %w", err)
		}
	}
	if result.Status != nil {
		if err := obj.Status.Decode(result.Status); err != nil {
			return fmt.Errorf("failed to decode status from YML: %w", err)
		}
	}

	s.Spec = result.Spec
	s.Status = result.Status

	return nil
}
