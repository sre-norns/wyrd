# SRE-Norns: Wyrd

[![Build](https://github.com/sre-norns/wyrd/actions/workflows/go.yml/badge.svg)](https://github.com/sre-norns/wyrd/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/sre-norns/wyrd.svg)](https://pkg.go.dev/github.com/sre-norns/wyrd)
[![Go Report Card](https://goreportcard.com/badge/sre-norns/wyrd)](https://goreportcard.com/report/github.com/sre-norns/wyrd)


A collection of reusable components for all our your SRE project needs.

# Usage
Install as a go-module:
```
go get github.com/sre-norns/wyrd
```

To get full benefits provided by this module:
- Define a model, using []() or [](). See [manifest](./pkg/manifest) docs for more details.
- Add middleware to your APIs to handle CRD request: search, get, update, delete etc. See [bark](./pkg/bark) for more info.
- Use `dbstore` to store and retrieve your previously defined models. See [dbstore](./pkg/dbstore)  for derails.


## Components
 * [grace](./pkg/grace) - A collection of utils for process init, signal handling and and graceful shutdown of services in a Cloud-native and local environment.
 * [manifest](./pkg/manifest) - utils to create Kubernetes-like Custom Resources Definition (CRD).
 * [bark](./pkg/bark) - collection of utils to help build REST APIs on top of [gin-gonic](https://gin-gonic.com) that operate with CRDs from manifest. It includes search middleware to create [labels-based query](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/).
 * [dbstore](./pkg/dbstore) - provides a store implementation with Object–relational mapping (ORM) that enables storage and search / query of the CRD resources with Relational DBs. Implemented on top of [GORM](https://gorm.io/) and thus feature the same [DB support](https://gorm.io/docs/connecting_to_the_database.html).   


## Name and meaning

From [Wikipedia](https://en.wikipedia.org/wiki/Wyrd):
> [Wyrd](https://en.wikipedia.org/wiki/Wyrd) is a concept in Anglo-Saxon culture roughly corresponding to fate or personal destiny. The word is ancestral to Modern English weird, whose meaning has drifted towards an adjectival use with a more general sense of "supernatural" or "uncanny", or simply "unexpected".


This go-module is a part of a large project `SRE-Norms` where each component is a play on terms _fate_, _future_ and _what is oat to be_.
It was moved into a stand-alone module out of project [Urth](https://github.com/sre-norns/urth) (WIP) prober-as-a-service.


### License

[Apache License, Version 2.0](http://www.apache.org/licenses/LICENSE-2.0)