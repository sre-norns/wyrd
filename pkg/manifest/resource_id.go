package manifest

import (
	"fmt"
	"strconv"
)

// Version type to represent monotonically increasing versions of a single resource.
type Version uint64

func (v Version) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

// ResourceID type represents an ID of a resource, duh!
type ResourceID uint

const InvalidResourceID ResourceID = 0

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

func (r VersionedResourceID) String() string {
	return fmt.Sprintf("%v@%d", r.ID, r.Version)
}
