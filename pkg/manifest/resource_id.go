package manifest

import (
	"fmt"
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

type ResourceModel[SpecType any] struct {
	ObjectMeta `json:",inline" yaml:",inline"`
	Spec       SpecType `json:"spec" yaml:"spec" gorm:"embedded"`
}

func (r *ResourceModel[SpecType]) ToManifest() ResourceManifest {
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
	if newEntry.Spec == nil {
		return ResourceModel[SpecType]{}, fmt.Errorf("spec is nil")
	}
	spec, ok := newEntry.Spec.(*SpecType)
	if !ok {
		return ResourceModel[SpecType]{}, fmt.Errorf("invalid spec type")
	}

	return ResourceModel[SpecType]{
		ObjectMeta: newEntry.Metadata,
		Spec:       *spec,
	}, nil
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
