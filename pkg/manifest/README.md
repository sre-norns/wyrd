# SRE-Norns: Wyrd/Manifest
A collection of components to help define flexible resource manifests.

It is _inspired_ by Kubernetes Custom Resource Definitions (CRD) and follow the spec closely.
This package enables service developer to support CRD format for the resources managed by their services.

# Usage

Suppose there is a type that represents a resource managed by your service:
```go
//....
type MySpec struct {
    Fullname  string `yaml:"name"`
    Address  string `yaml:"address"`
}
```

To support parsing and representing this resource in CRD format, this type must first be associated with a `Kind` value.

```go
// Somewhere in you code, run only once
// This can usually placed in init() for a module that has static types:

const KindMyType manifest.Kind = "mySpec"

func init() {
    // ...
	err := manifest.RegisterKind(KindMyType, &MySpec{})
    // handle errors
    //...
}
```

Once `"mySpec"` kind is registered, you can parse resource definitions:
```go
    //...
    fileContent, err := io.ReadAll(...)
    if err != nil {
        //... error handling
    }

    var resourceDefinition wyrd.ResourceManifest
	if err := yaml.Unmarshal(content, &resourceSpec); err != nil {
		//... error handling
	}

    if resourceDefinition.Kind == KindMyType {
        // Type case _should_ be safe but may fail due to implementation BUGS
        return resourceDefinition.Spec.(*MySpec)
    }

```
A manifest for a resource that can be unmarshaled into the type above would looks like:
```yaml
kind: mySpec
metadata:
    name: test-spec
spec:
    name: meaning
    address: "Knowhere"
```

Note that a resource manifest "wraps" type definition. A `ResourceManifest` consists of type meta information to help with marshaling,
information about the object instance, such as `UUID`, `Name` etc. and the custom type spec. itself.

```go
type ResourceManifest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     any        `json:"-" yaml:"-"`
}
```

## Labels
To make working with CRD-like resources easier, the package also includes helper functions to work with `Labels`. Any resource can have arbitrary (from CRD perspective) collection of "key-value" pairs attached to it. The package provides a definition of `LabelSelector` to ease implementation of resource that depends of other key-value labeled resources.

### Example
```go
type MySpecWithRequirements struct {
    Image string
    Requirements manifest.LabelSelector
}
```

The above spec definition might have the following YAML representation:
```yaml
kind: mySpec
metadata:
    name: test-spec
    labels: 
        env: test
        tier: ui
spec:
    requirements: 
        matchLabels:
            tier: test
        matchSelector:
            - { key: "env", operator: "NotIn", values: ["prod", "load-test"] }
    image: "my-service"
```

#### Implementation note
Types definitions provided in this package only help to define CRD but for full experience a Storage system must support querying resources based on labels. For example [manifest.LabelSelector] only defines serialization representation of selector but its storage system responsibility to find resources based on this requirements.
For users of [GORM](https://gorm.io) as their ORM layer, the library that helps to implement labels based selector is [dbStore](../dbstore/).
