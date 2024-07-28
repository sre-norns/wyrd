package manifest

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Version type to represent monotonically orderly versions of a single managed resource.
type Version uint64

// String returns string representation of the [Version] value
func (v Version) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

// ResourceID type represents unique Identifier of a resource in some namespace, duh!
// type ResourceID uuid.UUID
type ResourceID string

// InvalidResourceID represents nil value of a [ResourceID] which does not referrers to any resource in a system.
var InvalidResourceID ResourceID = ResourceID("")

var (
	ErrNilSpec           = fmt.Errorf(".spec is nil")
	ErrNilStatus         = fmt.Errorf(".status is nil")
	ErrSpecTypeInvalid   = fmt.Errorf("invalid manifest .spec type")
	ErrStatusTypeInvalid = fmt.Errorf("invalid manifest .status type")
)

// String returns string representation of the [ResourceID] value
// func (r ResourceID) String() string {
// 	// return strconv.FormatInt(int64(r), 10)
// 	// return uuid.UUID(r).String()
// }

// VersionedResourceID represents some versioned resources when its required to know not only UUID but exact version of it.
type VersionedResourceID struct {
	ID      ResourceID `form:"id" json:"id" yaml:"id" xml:"id"`
	Version Version    `form:"version" json:"version" yaml:"version" xml:"version"`
}

// NewVersionedID construct new VersionedResourceID from given ID and a version
func NewVersionedID(id ResourceID, version Version) VersionedResourceID {
	return VersionedResourceID{
		ID:      id,
		Version: version,
	}
}

// String returns string representation of the [VersionedResourceID] value in the `ID@version` format.
// For example: `132@3` - refers to the resource with ID 132, and specifically 3rd version of it.
func (r VersionedResourceID) String() string {
	return fmt.Sprintf("%v@%d", r.ID, r.Version)
}

type Model interface {
	GetKind() Kind
	GetTypeMetadata() TypeMeta
	GetMetadata() ObjectMeta
	GetSpec() any
	GetStatus() any
}

type ResourceModel[SpecType any] struct {
	ObjectMeta `json:",inline" yaml:",inline"`
	Spec       SpecType `json:"spec" yaml:"spec" gorm:"embedded"`

	// Part of semantic model - defines actions applicable to this model
	HResponse `json:",inline" yaml:",inline" gorm:"-"`
}

type StatefulResource[SpecType, StatusType any] struct {
	ObjectMeta `json:",inline" yaml:",inline"`
	Spec       SpecType `json:"spec" yaml:"spec" gorm:"embedded"`

	Status StatusType `json:"status,omitempty" yaml:"status,omitempty" gorm:"embedded;embeddedPrefix:status_"`

	// Part of semantic model - defines actions applicable to this model
	HResponse `json:",inline" yaml:",inline" gorm:"-"`
}

func ToManifest[SpecType any](r ResourceModel[SpecType]) ResourceManifest {
	spec := r.Spec
	return ResourceManifest{
		TypeMeta: TypeMeta{
			Kind: MustKnowKindOf(&spec),
		},
		Metadata: r.ObjectMeta,
		Spec:     &spec,
	}
}

func ManifestAsResource[SpecType any](newEntry ResourceManifest) (ResourceModel[SpecType], error) {
	result := ResourceModel[SpecType]{
		ObjectMeta: newEntry.Metadata,
	}

	manifestSpec, exist := LookupKind(newEntry.Kind)
	if !exist {
		return result, fmt.Errorf("%w: %q", ErrUnknownKind, newEntry.Kind)
	}

	if manifestSpec.SpecType != nil && newEntry.Spec == nil {
		return result, ErrNilSpec
	}
	if manifestSpec.SpecType == nil && newEntry.Spec != nil {
		return result, fmt.Errorf("%w: expected nill .spec", ErrSpecTypeInvalid)
	}

	// TODO: Can we cast base on manifestSpec.SpecType?
	if manifestSpec.SpecType != nil && newEntry.Spec != nil {
		if spec, ok := newEntry.Spec.(*SpecType); !ok {
			var t *SpecType
			return result, fmt.Errorf("%w: can't cast %v to %v", ErrSpecTypeInvalid, reflect.TypeOf(newEntry.Spec), reflect.TypeOf(t))
		} else {
			result.Spec = *spec
		}
	}

	return result, nil
}

func ManifestAsStatefulResource[SpecType, StatusType any](newEntry ResourceManifest) (StatefulResource[SpecType, StatusType], error) {
	result := StatefulResource[SpecType, StatusType]{
		ObjectMeta: newEntry.Metadata,
	}

	manifestSpec, exist := LookupKind(newEntry.Kind)
	if !exist {
		return result, fmt.Errorf("%w: %q", ErrUnknownKind, newEntry.Kind)
	}

	if manifestSpec.SpecType != nil && newEntry.Spec == nil {
		return result, ErrNilSpec
	}
	if manifestSpec.SpecType == nil && newEntry.Spec != nil {
		return result, ErrSpecTypeInvalid
	}

	if manifestSpec.StatusType != nil && newEntry.Status == nil {
		return result, ErrNilStatus
	}
	if manifestSpec.StatusType == nil && newEntry.Status != nil {
		return result, ErrStatusTypeInvalid
	}

	if manifestSpec.SpecType != nil && newEntry.Spec != nil {
		if spec, ok := newEntry.Status.(*SpecType); !ok {
			return result, ErrSpecTypeInvalid
		} else {
			result.Spec = *spec
		}
	}

	if manifestSpec.StatusType != nil && newEntry.Status != nil {
		if status, ok := newEntry.Status.(*StatusType); !ok {
			return result, ErrStatusTypeInvalid
		} else {
			result.Status = *status
		}
	}

	return result, nil
}

func (r *ResourceModel[SpecType]) BeforeCreate(tx *gorm.DB) (err error) {
	if r.UID == "" {
		r.UID = ResourceID(uuid.NewString())
	}

	if r.Name == "" {
		r.Name = string(r.UID)
	}

	return
}
