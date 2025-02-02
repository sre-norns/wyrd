package manifest

import (
	"errors"
	"regexp"
	"strconv"
)

var (
	ErrNameTooLong    = errors.New("name is too long")
	ErrNameNotDNSname = errors.New("name is not a DNS subdomain name")
)

// Version type to represent monotonically orderly versions of a single managed resource.
type Version uint64

// String returns string representation of the [Version] value
func (v Version) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

// ResourceID type represents unique Identifier of a resource in a system.
// type ResourceID uuid.UUID
type ResourceID string

// ResourceName type represents unique name of a resource in a namespace, duh!
type ResourceName string

// InvalidResourceID represents nil value of a [ResourceID] which does not referrers to any resource in a system.
var InvalidResourceID ResourceID = ResourceID("")

var subdomainNameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9\.\-]*[a-z0-9])?$`)

func ValidateSubdomainName(value string) error {
	if len(value) > 253 {
		return ErrNameTooLong
	}

	// contain only lowercase alphanumeric characters, '-' or '.'
	// start with an alphanumeric character
	// end with an alphanumeric character
	if !subdomainNameRegexp.MatchString(value) {
		return ErrNameNotDNSname
	}

	return nil
}

func (name ResourceName) ValidateSubdomainName() error {
	return ValidateSubdomainName(string(name))
}
