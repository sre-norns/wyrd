package manifest

import (
	"fmt"
	"strconv"
)

// Version type to represent monotonically orderly versions of a single managed resource.
type Version uint64

// String returns string representation of the [Version] value
func (v Version) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

// ResourceID type represents an ID of a resource, duh!
type ResourceID uint

// InvalidResourceID represents nil value of a [ResourceID] which does not referrers to any resource in a system.
const InvalidResourceID ResourceID = 0

// String returns string representation of the [ResourceID] value
func (r ResourceID) String() string {
	return strconv.FormatInt(int64(r), 10)
}

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
