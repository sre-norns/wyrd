package mail

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/mailgun/mailgun-go/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var mailgunDomainPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$`)

// NewMailgun creates a sender. The transport permits isolated SDK tests; deployment endpoints remain fixed.
func NewMailgun(cfg Config, transport http.RoundTripper) (func(context.Context, string, string, string) error, error) {
	if len(cfg.MailgunDomain) > 253 || !mailgunDomainPattern.MatchString(cfg.MailgunDomain) {
		return nil, fmt.Errorf("email provider Mailgun requires a valid sending domain")
	}
	if strings.TrimSpace(cfg.MailgunAPIKey) == "" || strings.TrimSpace(cfg.MailgunAPIKey) != cfg.MailgunAPIKey {
		return nil, fmt.Errorf("email provider Mailgun requires an API key without surrounding whitespace")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil || from.Address != cfg.From || strings.ContainsAny(cfg.From, "\r\n") {
		return nil, fmt.Errorf("email provider Mailgun sender must be an email address")
	}
	base := mailgun.APIBaseUS
	switch cfg.MailgunRegion {
	case "", "us":
	case "eu":
		base = mailgun.APIBaseEU
	default:
		return nil, fmt.Errorf("email provider Mailgun region must be us or eu")
	}
	client := mailgun.NewMailgun(cfg.MailgunAPIKey)
	if err := client.SetAPIBase(base); err != nil {
		return nil, fmt.Errorf("invalid Mailgun region endpoint")
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	// The traced transport is a no-op unless trace export is configured.
	client.SetHTTPClient(&http.Client{Transport: otelhttp.NewTransport(transport), Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
	return func(ctx context.Context, to, subject, body string) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		message := mailgun.NewMessage(cfg.MailgunDomain, cfg.From, subject, body, to)
		// Account-access links must not be rewritten by tracking services.
		message.SetTracking(false)
		message.SetTrackingClicks(false)
		message.SetTrackingOpens(false)
		_, err := client.Send(ctx, message)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Provider errors can include request data. Keep secrets and reset links out of errors.
			return errors.New("email provider Mailgun could not accept account email")
		}
		return nil
	}, nil
}
