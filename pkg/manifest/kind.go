package manifest

import (
	"errors"
	"reflect"
)

// Kind represents ID of a type that can be used as a spec in a manifest
type Kind string

// KindSpec defines mapping of a manifest to types
type KindSpec struct {
	SpecType   reflect.Type
	StatusType reflect.Type
}

var (
	// ErrUnknownKind is the error returned when the 'kind' value goes not match any previously registered type.
	ErrUnknownKind = errors.New("unknown kind")
	// ErrUnexpectedSpecType is an error returned when type cast of a .spec in the manifest is not possible to the expected type.
	ErrUnexpectedSpecType = errors.New("unexpected spec type")
	// ErrUninterfacableType is an error returned when the type being registered can not be captured by interface.
	ErrUninterfacableType = errors.New("type can not interface")
)
