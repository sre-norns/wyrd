# SRE-Norns: Wyrd
A collection of reusable components for all our your SRE project needs

# Usage
Use as a standard go module.

## Components
 * [grace](./pkg/grace) - A collection of utils for process init, signal handling and and graceful shutdown of services in a Cloud-native and local environment.
 * [manifest](./pkg/manifest) - Kubernetes inspired (aim is not be compatible) custom resources definition.
 * [bark](./pkg/bark) - collection of utils to help build reach REST APIs on top of [gin-gonic](TBD) golang web framework.

