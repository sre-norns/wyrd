// Package authz provides registered resource authorization and wyrd store visibility.
package authz

import (
	"github.com/sre-norns/wyrd/identity"
)

type Visibility = identity.Visibility
type Kind = identity.Kind

var Authorize = identity.Authorize
var WithPrincipal = identity.WithPrincipal
var WithServicePrincipal = identity.WithServicePrincipal
