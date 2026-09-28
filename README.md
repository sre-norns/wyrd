# SRE-Norns: Wyrd

[![Build](https://github.com/sre-norns/wyrd/actions/workflows/go.yml/badge.svg)](https://github.com/sre-norns/wyrd/actions/workflows/go.yml)
[![CodeQL](https://github.com/sre-norns/wyrd/actions/workflows/codeql.yml/badge.svg)](https://github.com/sre-norns/wyrd/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/sre-norns/wyrd.svg)](https://pkg.go.dev/github.com/sre-norns/wyrd)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

A collection of reusable components for all your SRE project needs.

Wyrd is the toolkit for services that manage _resources_: define them as Kubernetes-like Custom Resource
Definitions (CRD), serve them over a REST API, store and query them by labels, and shut the whole thing
down gracefully when Kubernetes sends `SIGTERM`. Each package is useful on its own, and they compose.

## Requirements

Go 1.26 or newer (see [go.mod](./go.mod)).

## Install

```sh
go get github.com/sre-norns/wyrd
```

## Quickstart

Associate your own type with a CRD `kind`, and the manifest parser will produce it for you:

```go
package main

import (
	"fmt"
	"log"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gopkg.in/yaml.v3"
)

// ServiceSpec is a resource type managed by your service.
type ServiceSpec struct {
	Image    string `json:"image" yaml:"image"`
	Replicas int    `json:"replicas" yaml:"replicas"`
}

// KindService associates the type above with a CRD `kind` value.
const KindService manifest.Kind = "service"

func init() {
	manifest.MustRegisterKind(KindService, &ServiceSpec{})
}

var doc = []byte(`
kind: service
metadata:
  name: web-frontend
  labels:
    env: prod
    tier: ui
spec:
  image: "my-service:1.2.3"
  replicas: 3
`)

func main() {
	var resource manifest.ResourceManifest
	if err := yaml.Unmarshal(doc, &resource); err != nil {
		log.Fatal(err)
	}

	spec, ok := resource.Spec.(*ServiceSpec)
	if !ok {
		log.Fatalf("unexpected kind: %q", resource.Kind)
	}
	fmt.Printf("%s: %d x %s\n", resource.Metadata.Name, spec.Replicas, spec.Image)

	// Resources carry labels, and labels can be selected on - the same way `kubectl` does it.
	selector, err := manifest.ParseSelector("env in (prod, staging), tier=ui")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("matches selector: %t\n", selector.Matches(resource.Metadata.Labels))
}
```

```
web-frontend: 3 x my-service:1.2.3
matches selector: true
```

From there:

- Serve the resource over HTTP with [bark](./pkg/bark) middleware: content negotiation, pagination and
  label-based search come for free.
- Persist and query it with [dbstore](./pkg/dbstore), which turns the same label selectors into SQL.
- Notify other services of changes with [webhooks](./pkg/webhooks).
- Wire up process startup and graceful shutdown with [grace](./pkg/grace).

## Packages

| Package | What it is for |
| --- | --- |
| [manifest](./pkg/manifest) | Kubernetes-like Custom Resource Definitions (CRD): type registry, resource metadata, labels and [label selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/). |
| [bark](./pkg/bark) | REST API building blocks for [gin-gonic](https://gin-gonic.com) that operate on manifest resources: content-type negotiation, search and pagination middleware, versioned resource handlers. |
| [dbstore](./pkg/dbstore) | Storage and label-based search of CRD resources in relational databases. Built on [GORM](https://gorm.io/), so it supports the same [databases](https://gorm.io/docs/connecting_to_the_database.html). |
| [idempotency](./pkg/idempotency) | The record store behind `bark.Idempotent`: safe retries by `Idempotency-Key`. |
| [grace](./pkg/grace) | Process lifecycle: OS signal handling for graceful shutdown in Kubernetes, startup assertions, and a limited-concurrency workgroup. |
| [webhooks](./pkg/webhooks) | Webhook resource definition and an HTTP caller to notify subscribers of resource changes. |

## Development

```sh
make test        # go test with the race detector; set WYRD_TEST_POSTGRES_URL to include Postgres tests
make test/cover  # ... and open the coverage report
make audit       # go mod verify, go vet, staticcheck and tests
make scan-vuln   # govulncheck against the Go vulnerability database
make tidy        # gofmt and go mod tidy
make help        # list all targets
```

## Contributing

Issues and pull requests are welcome. Please run `make audit` before opening a PR: it is the same set of
checks CI runs, so it is the quickest way to get a green build. New behaviour is expected to come with
tests - the suite runs under `-race`, so concurrent code gets exercised there too.

## Name and meaning

From [Wikipedia](https://en.wikipedia.org/wiki/Wyrd):
> [Wyrd](https://en.wikipedia.org/wiki/Wyrd) is a concept in Anglo-Saxon culture roughly corresponding to fate or personal destiny. The word is ancestral to Modern English weird, whose meaning has drifted towards an adjectival use with a more general sense of "supernatural" or "uncanny", or simply "unexpected".

This go-module is a part of a larger project `SRE-Norns` where each component is a play on the terms _fate_, _future_ and _what is ought to be_.
It was moved into a stand-alone module out of the project [Urth](https://github.com/sre-norns/urth) (WIP) prober-as-a-service.

## License

[Apache License, Version 2.0](./LICENSE)

## Identity module

[Identity and tenancy](identity/README.md) is an independent nested Go module,
`github.com/sre-norns/wyrd/identity`. It contains the identity implementation
extracted from Exp-Bench. Root-module consumers do not inherit its OAuth and mail
dependencies. Its releases use `identity/v*` tags. The extracted module retains
its source license; see [identity/LICENSE](identity/LICENSE).
