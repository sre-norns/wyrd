// Command fake-idp serves a local fake Google and GitHub identity provider for
// browser tests and local development of any product using wyrd/identity. Its
// Google issuer also serves OpenID discovery, so it stands in for a generic
// OIDC provider too. It listens only on a loopback address and never contacts
// a real provider. Do not run it in production.
//
//	go run github.com/sre-norns/wyrd/identity/cmd/fake-idp
package main

import (
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/sre-norns/wyrd/identity/fakeidp"
)

// envOr lets a flag default come from the environment, so a Makefile or a
// test runner can configure the fake without assembling a command line.
func envOr(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func main() {
	listen := flag.String("listen", envOr("FAKE_IDP_LISTEN", "127.0.0.1:18090"), "Loopback listen address (FAKE_IDP_LISTEN)")
	clientID := flag.String("client-id", envOr("FAKE_IDP_CLIENT_ID", "fake-client"), "Client ID that the fake accepts (FAKE_IDP_CLIENT_ID)")
	clientSecret := flag.String("client-secret", envOr("FAKE_IDP_CLIENT_SECRET", "fake-secret"), "Client secret that the fake accepts (FAKE_IDP_CLIENT_SECRET)")
	flag.Parse()

	host, _, err := net.SplitHostPort(*listen)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		slog.Error("The fake identity provider listens only on a loopback IP address.")
		os.Exit(1)
	}
	fake := fakeidp.New(*clientID, *clientSecret)
	fake.URL = "http://" + *listen
	server := &http.Server{Addr: *listen, Handler: fake, ReadHeaderTimeout: 5 * time.Second}
	slog.Info("Fake identity provider ready",
		"google_issuer", fake.GoogleIssuer(),
		"oidc_issuer", fake.GoogleIssuer(),
		"github_web", fake.GitHubWeb(),
		"github_api", fake.GitHubAPI())
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Fake identity provider failed", "err", err.Error())
		os.Exit(1)
	}
}
