package identity

import (
	"net/http"
)

func ProviderCallback(issuer, name string) string { return providerCallback(issuer, name) }

type StorageProviderIdentity = providerIdentity
type StorageUpstreamProvider = upstreamProvider

const ValueProviderExchangeFailed = providerExchangeFailed
const ValueProviderInvalidIdentity = providerInvalidIdentity
const ValueProviderEmailUnverified = providerEmailUnverified
const ValueProviderUnavailable = providerUnavailable

func ProviderCategory(err error) string { return providerCategory(err) }

const ValueProviderResponseLimit = providerResponseLimit

var ValueErrProviderResponseTooLarge = errProviderResponseTooLarge

func ProviderHTTPClient(provider string, base http.RoundTripper) *http.Client {
	return providerHTTPClient(provider, base)
}

// ProviderLabel returns the display name of a configured provider.
func ProviderLabel(name string) string { return providerLabel(name) }
