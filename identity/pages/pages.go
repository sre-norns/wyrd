package pages

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	server "github.com/sre-norns/wyrd/identity"
)

//go:embed pages/oauth.css
var oauthStyle string

//go:embed pages/oauth.html
var oauthHTML string

//go:embed pages/password.js
var passwordScript string

// ValidPrivacyURL accepts an empty value, an absolute https URL, or a path on
// this host.
func ValidPrivacyURL(value string) error {
	if value == "" || (strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")) {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("privacy URL %q must be an https URL or a path that starts with /", value)
	}
	return nil
}

var Pages = template.Must(template.New("oauth").Funcs(template.FuncMap{"providerLabel": server.ProviderLabel, "headlineVariants": headlineVariants}).Parse(oauthHTML))

// headlineVariants is the JSON the script reads from the headline's
// data-variants attribute; html/template escapes it for the attribute.
func headlineVariants(variants []string) string {
	if len(variants) < 2 {
		return ""
	}
	encoded, _ := json.Marshal(variants)
	return string(encoded)
}

// Config brands the pages for one product. ThemeCSS is appended to the
// stylesheet; pass the product's @sre-norns/components theme :root block.
type Config struct {
	ProductName, PrivacyURL, ThemeCSS string
	// Copy is the product's wording. Empty fields keep DefaultCopy's.
	Copy Copy
}

// Copy is the wording on the pages that belongs to a product rather than to
// identity. The defaults are the wording the pages were written with.
type Copy struct {
	// Tagline is the eyebrow beside the forms and the footer's description.
	Tagline string
	// Headline heads the sign-in, registration, recovery and invitation pages,
	// and titles the device-approval result.
	Headline string
	// HeadlineVariants are the rewordings the pages' script crossfades the
	// headline through, at an unpredictable pace. Fewer than two variants keep
	// the headline still. The default variants go with the default headline
	// only: a product with its own headline and none of its own keeps it still.
	HeadlineVariants []string
	// AuthorizeHeadline heads the page that approves an application's access.
	AuthorizeHeadline string
	// Description follows the headline.
	Description string
	// InvitationDescription follows the headline of an account invitation.
	InvitationDescription string
	// MachineTokens names the machine credentials an account deactivation revokes.
	MachineTokens string
	// DeviceRetry tells a person whose device sign-in can no longer complete
	// how to start another. Only the product knows which tool began it.
	DeviceRetry string
}

var DefaultCopy = Copy{
	Tagline:  "Continuous experimentation",
	Headline: "Experience the progress.",
	HeadlineVariants: []string{
		"Experience the progress.",
		"Experience the progress",
		"Experience the progress…",
		"Experience the process.",
		"Experiment in progress.",
		"Improvement in progress.",
		"Improvement in process.",
		"Embrace the progress.",
		"Embrace the experiment",
		"Experiment with the process.",
		"Progress, in progress.",
		"Progress the experience",
		"Progress the process",
	},
	AuthorizeHeadline:     "Connect to your research.",
	Description:           "Configure projects. Inspect evidence. Track progress toward your objectives.",
	InvitationDescription: "Join a research account. Your other account memberships stay unchanged.",
	MachineTokens:         "agent tokens",
	DeviceRetry:           "Run expbctl auth login again.",
}

// OrDefault fills the fields a product left empty from DefaultCopy.
func (c Copy) OrDefault() Copy {
	if c.Headline == "" && c.HeadlineVariants == nil {
		c.HeadlineVariants = DefaultCopy.HeadlineVariants
	}
	for _, f := range []struct {
		value    *string
		fallback string
	}{
		{&c.Tagline, DefaultCopy.Tagline},
		{&c.Headline, DefaultCopy.Headline},
		{&c.AuthorizeHeadline, DefaultCopy.AuthorizeHeadline},
		{&c.Description, DefaultCopy.Description},
		{&c.InvitationDescription, DefaultCopy.InvitationDescription},
		{&c.MachineTokens, DefaultCopy.MachineTokens},
		{&c.DeviceRetry, DefaultCopy.DeviceRetry},
	} {
		if *f.value == "" {
			*f.value = f.fallback
		}
	}
	return c
}

type Page struct {
	ProductName string
	Copy        Copy
	Invitation  *server.InvitationPage
	server.BrowserAuthorization
	AccessForm      string
	Token           string
	AccountName     string
	WebLogin        bool
	AccountArchived bool
	Title           string
	Style           template.CSS
	Script          template.JS
	Device          bool
	Failed          bool
	Message         string
	Retry           string
	RetryLabel      string
	// Action is the form target when the page URL is not the next step, for
	// example after a provider callback.
	Action string
	// Provider is the safe provider name of the current page.
	Provider string
	// Confirmation describes a pending provider confirmation.
	Confirmation *server.ProviderConfirmationPage
	// Fields holds field errors that the page shows next to the input.
	Fields map[string]string
	// PrivacyURL is the privacy notice link of the footer.
	PrivacyURL string
}

func Render(ctx *gin.Context, status int, name string, page Page, cfg Config) {
	// The style and script are embedded application code. CSP permits their exact hashes.
	style := oauthStyle + cfg.ThemeCSS
	page.Style = template.CSS(style)
	page.ProductName = cfg.ProductName
	page.Copy = cfg.Copy.OrDefault()
	if page.ProductName == "" {
		page.ProductName = "SRE-Norns"
	}
	page.Script = template.JS(passwordScript)
	page.PrivacyURL = cfg.PrivacyURL
	scriptSum := sha256.Sum256([]byte(passwordScript))
	sum := sha256.Sum256([]byte(style))
	ctx.Header("Content-Security-Policy", "default-src 'none'; style-src 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(scriptSum[:])+"'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("Pragma", "no-cache")
	ctx.Header("Referrer-Policy", "no-referrer")
	ctx.Status(status)
	_ = Pages.ExecuteTemplate(ctx.Writer, name, page)
}

// RetryFor rebuilds a restart URL from the safe outer authorization fields.
func RetryFor(path string, form url.Values) string {
	values := url.Values{}
	for _, key := range []string{"client_id", "redirect_uri", "response_type", "code_challenge_method", "code_challenge", "state", "user_code", "preferred_context"} {
		if value := form.Get(key); value != "" {
			values.Set(key, value)
		}
	}
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}

// outerAuthorization returns the safe outer authorization fields of the page.
func (p Page) outerAuthorization() url.Values {
	values := url.Values{}
	if p.ClientID != "" && p.RedirectURI != "" {
		values.Set("client_id", p.ClientID)
		values.Set("redirect_uri", p.RedirectURI)
		values.Set("response_type", "code")
		values.Set("code_challenge_method", "S256")
		values.Set("code_challenge", p.Challenge)
		values.Set("state", p.State)
	}
	if p.PreferredContext != "" {
		values.Set("preferred_context", p.PreferredContext)
	}
	return values
}

// OuterQuery carries the outer authorization to a linked page, such as
// Create account, so that a later sign-in resumes the same transaction.
func (p Page) OuterQuery() string {
	if values := p.outerAuthorization(); values.Get("client_id") != "" {
		return "?" + values.Encode()
	}
	return ""
}

// ShowProviders reports whether the page offers provider sign-in. A page that
// already holds an authentication proof does not. A device page needs the
// device code to bind the provider transaction.
func (p Page) ShowProviders() bool {
	return len(p.Providers) > 0 && p.AuthTicket == "" && (!p.Device || p.UserCode != "")
}

// ProviderLink is the same-origin start URL of a provider for this page.
func (p Page) ProviderLink(name string) string {
	if p.Invitation != nil {
		return "/oauth/invitations/providers/" + url.PathEscape(name) + "/start"
	}
	values := p.outerAuthorization()
	switch {
	case p.AccessForm == "register":
		values.Set("purpose", "register")
	case p.Device:
		values = url.Values{"purpose": {"device"}, "user_code": {p.UserCode}}
	case p.WebLogin:
		values.Set("purpose", "login")
	default:
		values.Set("purpose", "authorize")
	}
	return "/oauth/providers/" + url.PathEscape(name) + "/start?" + values.Encode()
}
