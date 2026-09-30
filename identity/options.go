package identity

import (
	"time"

	identitymail "github.com/sre-norns/wyrd/identity/mail"
)

// Options is embeddable in a Kong command. Use embed:"" envprefix:"URTH_"
// (or the product prefix) on the embedding field. Exp-Bench keeps its existing
// flat flag adapter so legacy environment aliases remain valid.
type Options struct {
	Issuer                  string        `default:"http://localhost:8080" env:"ISSUER"`
	AccountProvisioning     string        `default:"self-service" enum:"self-service,system-admin-only" env:"ACCOUNT_PROVISIONING"`
	Development             bool          `env:"DEVELOPMENT"`
	AccessDuration          time.Duration `default:"15m" env:"ACCESS_DURATION"`
	RefreshDuration         time.Duration `default:"720h" env:"REFRESH_DURATION"`
	AuthenticationRateLimit int64         `default:"120" env:"AUTHENTICATION_RATE_LIMIT"`
	GoogleClientID          string        `env:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret      string        `env:"GOOGLE_CLIENT_SECRET"`
	GitHubClientID          string        `name:"github-client-id" env:"GITHUB_CLIENT_ID"`
	GitHubClientSecret      string        `name:"github-client-secret" env:"GITHUB_CLIENT_SECRET"`
	OIDCIssuerURL           string        `env:"OIDC_ISSUER_URL"`
	OIDCClientID            string        `env:"OIDC_CLIENT_ID"`
	OIDCClientSecret        string        `env:"OIDC_CLIENT_SECRET"`
	MailProvider            string        `default:"auto" enum:"auto,smtp,mailgun,development" env:"MAIL_PROVIDER"`
	MailFrom                string        `env:"MAIL_FROM"`
	MailDirectory           string        `env:"AUTH_MAIL_DIR"`
	SMTPAddress             string        `env:"SMTP_ADDRESS"`
	SMTPUsername            string        `env:"SMTP_USERNAME"`
	SMTPPassword            string        `env:"SMTP_PASSWORD"`
	MailgunDomain           string        `env:"MAILGUN_DOMAIN"`
	MailgunAPIKey           string        `env:"MAILGUN_API_KEY"`
	MailgunRegion           string        `default:"us" enum:"us,eu" env:"MAILGUN_REGION"`
}

// Apply overlays runtime options on branded defaults and configures mail.
func (o Options) Apply(c Config) (Config, error) {
	c.Issuer = o.Issuer
	c.AccountProvisioning = o.AccountProvisioning
	c.Development = o.Development
	c.AccessDuration = o.AccessDuration
	c.RefreshDuration = o.RefreshDuration
	c.AuthenticationRateLimit = o.AuthenticationRateLimit
	c.Providers = map[string]ProviderConfig{"google": {ClientID: o.GoogleClientID, ClientSecret: o.GoogleClientSecret}, "github": {ClientID: o.GitHubClientID, ClientSecret: o.GitHubClientSecret}, "oidc": {IssuerURL: o.OIDCIssuerURL, ClientID: o.OIDCClientID, ClientSecret: o.OIDCClientSecret}}
	sender, err := identitymail.New(identitymail.Config{Provider: o.MailProvider, From: o.MailFrom, Development: o.Development, DevelopmentDirectory: o.MailDirectory, SMTPAddress: o.SMTPAddress, SMTPUsername: o.SMTPUsername, SMTPPassword: o.SMTPPassword, MailgunDomain: o.MailgunDomain, MailgunAPIKey: o.MailgunAPIKey, MailgunRegion: o.MailgunRegion})
	if err != nil {
		return c, err
	}
	c.SendIdentityMail = sender
	return c, nil
}
