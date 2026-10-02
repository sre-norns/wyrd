package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// RequestOptions supplies controls that are not part of resource bodies.
// Patch, when non-nil, contains only the fields to change, including zero values.
// Reuse IdempotencyKey when retrying the same POST operation.
type RequestOptions struct {
	Sort            string
	Direction       string
	IdempotencyKey  string
	IfMatch         string
	InvitationToken string
	Patch           map[string]json.RawMessage
	OAuthQuery      url.Values
	// Header and Query carry a product's own request controls -- a lease
	// token, a list filter -- through the same options.
	Header http.Header
	Query  url.Values
}
type optionsKey struct{}

func WithRequestOptions(ctx context.Context, options RequestOptions) context.Context {
	return context.WithValue(ctx, optionsKey{}, options)
}

// Options returns the RequestOptions attached to ctx.
func Options(ctx context.Context) RequestOptions {
	options, _ := ctx.Value(optionsKey{}).(RequestOptions)
	return options
}

// Problem describes a rejected request, including server error details and retry information.
type Problem struct {
	Type       string            `json:"type"`
	Title      string            `json:"title"`
	Status     int               `json:"status"`
	Code       string            `json:"code"`
	Detail     string            `json:"detail"`
	Instance   string            `json:"instance,omitempty"`
	RequestID  string            `json:"requestId,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
	RetryAfter string            `json:"-"`
}

func (p *Problem) Error() string { return fmt.Sprintf("HTTP %d (%s): %s", p.Status, p.Code, p.Detail) }

// OAuthResponse retains JSON, browser HTML, or an authorization redirect.
type OAuthResponse struct {
	Body        string
	Location    string
	ContentType string
	Status      int
}

// ResourcePath joins a route prefix with path-escaped segments.
func ResourcePath(parts ...string) string {
	escaped := make([]string, len(parts))
	for i, part := range parts {
		if i == 0 {
			escaped[i] = strings.Trim(part, "/")
		} else {
			escaped[i] = url.PathEscape(part)
		}
	}
	return "/" + strings.Join(escaped, "/")
}

// SearchValues is the query of a list request. Lists page by cursor only, so
// an offset is refused before anything is sent.
func SearchValues(query manifest.SearchQuery) (url.Values, error) {
	if query.Offset != 0 {
		return nil, &Problem{Status: http.StatusBadRequest, Code: "offset-unsupported", Detail: "Lists page by cursor only: pass the previous page's next as cursor."}
	}
	values := url.Values{}
	if query.Fields != nil && !query.Fields.Empty() {
		values.Set("fields", query.Fields.String())
	}
	if query.Selector != nil && !query.Selector.Empty() {
		values.Set("labels", query.Selector.String())
	}
	if query.Name != "" {
		values.Set("name", query.Name)
	}
	if !query.FromTime.IsZero() {
		values.Set("from", query.FromTime.Format(time.RFC3339Nano))
	}
	if !query.TillTime.IsZero() {
		values.Set("till", query.TillTime.Format(time.RFC3339Nano))
	}
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Limit != 0 {
		values.Set("limit", strconv.FormatUint(uint64(query.Limit), 10))
	}
	return values, nil
}

// Exchange owns request construction, deadlines, credentials, and response closure.
// It does not retry mutations or follow redirects with credentials. An OAuth
// exchange carries no resource credential and reads OAuth's error shape.
func (c *Client) Exchange(ctx context.Context, method, path string, query url.Values, body any, oauth bool) ([]byte, int, http.Header, error) {
	if c.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.config.Timeout)
		defer cancel()
	}
	options := Options(ctx)
	if !oauth && len(options.Query) > 0 {
		merged := url.Values{}
		for key, values := range query {
			merged[key] = values
		}
		for key, values := range options.Query {
			merged[key] = values
		}
		query = merged
	}
	endpoint := c.baseURL.String() + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var data []byte
	contentType := "application/json"
	var err error
	if form, ok := body.(url.Values); ok {
		data = []byte(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("encode request: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if !oauth {
		if c.config.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.config.Token)
		}
		if method == http.MethodPost {
			key := options.IdempotencyKey
			if key == "" {
				key = uuid.NewString()
			}
			req.Header.Set("Idempotency-Key", key)
		}
		for name, value := range map[string]string{"If-Match": options.IfMatch, "X-Invitation-Token": options.InvitationToken} {
			if value != "" {
				req.Header.Set(name, value)
			}
		}
		for name, values := range options.Header {
			for _, value := range values {
				req.Header.Add(name, value)
			}
		}
	}
	transport := *c.config.HTTPClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := transport.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer res.Body.Close()
	data, err = io.ReadAll(io.LimitReader(res.Body, 32<<20+1))
	if err != nil {
		return nil, res.StatusCode, res.Header, fmt.Errorf("read response: %w", err)
	}
	if len(data) > 32<<20 {
		return nil, res.StatusCode, res.Header, errors.New("response exceeds 32 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if oauth && res.StatusCode >= 300 && res.StatusCode < 400 {
			return data, res.StatusCode, res.Header, nil
		}
		p := &Problem{}
		_ = json.Unmarshal(data, p)
		if oauth {
			var failure struct {
				Code   string `json:"error"`
				Detail string `json:"error_description"`
			}
			_ = json.Unmarshal(data, &failure)
			p.Code, p.Detail = failure.Code, failure.Detail
		}
		p.Status = res.StatusCode
		if p.Title == "" {
			p.Title = http.StatusText(p.Status)
		}
		if p.Code == "" {
			p.Code = "http-error"
		}
		if p.Detail == "" {
			p.Detail = p.Title
		}
		if p.RequestID == "" {
			p.RequestID = res.Header.Get("X-Request-ID")
		}
		p.RetryAfter = res.Header.Get("Retry-After")
		return nil, res.StatusCode, res.Header, p
	}
	return data, res.StatusCode, res.Header, nil
}

// ResourceClient reads and writes one resource type over a Client's transport.
type ResourceClient[T any] struct{ client *Client }

func Resource[T any](client *Client) ResourceClient[T] { return ResourceClient[T]{client} }

// Request sends one request and decodes the response. A 204 decodes nothing.
func (r ResourceClient[T]) Request(ctx context.Context, method, path string, query url.Values, body any) (value T, status int, err error) {
	if resource.IsResource(body) {
		body, err = resource.Input(body, method == http.MethodPatch)
		if err != nil {
			return value, 0, err
		}
	}
	data, status, _, err := r.client.Exchange(ctx, method, path, query, body, false)
	if err != nil || status == http.StatusNoContent {
		return value, status, err
	}
	if err = resource.Decode(data, &value); err != nil {
		return value, status, fmt.Errorf("decode response: %w", err)
	}
	return
}
func (r ResourceClient[T]) Get(ctx context.Context, path string) (T, bool, error) {
	value, status, err := r.Request(ctx, http.MethodGet, path, nil, nil)
	if status == http.StatusNotFound {
		return value, false, nil
	}
	return value, err == nil && status != http.StatusNoContent, err
}
func (r ResourceClient[T]) List(ctx context.Context, path string, query manifest.SearchQuery) ([]T, manifest.Page, error) {
	values, err := SearchValues(query)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	options := Options(ctx)
	if options.Sort != "" {
		values.Set("sort", options.Sort)
	}
	if options.Direction != "" {
		values.Set("direction", options.Direction)
	}
	return r.ListValues(ctx, path, values)
}

// ListValues lists with a query already built, for lists that take more than
// a SearchQuery says.
func (r ResourceClient[T]) ListValues(ctx context.Context, path string, values url.Values) ([]T, manifest.Page, error) {
	var envelope struct {
		Items []T `json:"items"`
		manifest.Page
	}
	data, _, _, err := r.client.Exchange(ctx, http.MethodGet, path, values, nil, false)
	if err == nil {
		err = resource.Decode(data, &envelope)
	}
	return envelope.Items, envelope.Page, err
}
func (r ResourceClient[T]) Post(ctx context.Context, path string, body any) (T, error) {
	value, _, err := r.Request(ctx, http.MethodPost, path, nil, body)
	return value, err
}
func (r ResourceClient[T]) PostStatus(ctx context.Context, path string, body any) (T, bool, error) {
	value, status, err := r.Request(ctx, http.MethodPost, path, nil, body)
	return value, err == nil && status == http.StatusCreated, err
}
func (r ResourceClient[T]) PostFound(ctx context.Context, path string, body any) (T, bool, error) {
	value, status, err := r.Request(ctx, http.MethodPost, path, nil, body)
	if status == http.StatusNotFound {
		return value, false, nil
	}
	return value, err == nil, err
}

// Patch updates the fields of value a client may change, guarded by its
// revision. Identity types name their own mutable fields; any other type
// changes its name, labels and status. A product type with other mutable
// fields uses PatchFields.
func (r ResourceClient[T]) Patch(ctx context.Context, path string, value T) (out T, created bool, err error) {
	return r.PatchFields(ctx, path, value, mutableFields(reflect.TypeOf(value).Name()))
}

// PatchFields is Patch with the mutable fields named. RequestOptions.Patch,
// when set, is sent as it is instead.
func (r ResourceClient[T]) PatchFields(ctx context.Context, path string, value T, mutable []string) (out T, created bool, err error) {
	meta, ok := any(&value).(interface{ Metadata() *model.Resource })
	if !ok || meta.Metadata().ID == "" {
		return out, false, errors.New("resource ID is required for an update")
	}
	options := Options(ctx)
	if options.IfMatch == "" && meta.Metadata().Revision > 0 {
		options.IfMatch = fmt.Sprintf("\"%d\"", meta.Metadata().Revision)
	}
	if resource.IsResource(value) {
		var body any = options.Patch
		if options.Patch == nil {
			// Credential revocation remains available through the existing Go service
			// method; the wire always uses an explicit command.
			switch any(value).(type) {
			case model.Session, model.AgentIdentityToken, model.AccountInvitation:
				if meta.Metadata().Status != "revoked" {
					return out, false, errors.New("credential updates require revoke")
				}
				body, err = resource.Command(value, "revoke")
			default:
				body, err = resource.Input(value, true)
			}
			if err != nil {
				return out, false, err
			}
		}
		out, status, err := r.Request(WithRequestOptions(ctx, options), http.MethodPatch, path, nil, body)
		return out, err == nil && status == http.StatusCreated, err
	}
	fields := options.Patch
	if fields == nil {
		data, err := json.Marshal(value)
		if err != nil {
			return out, false, err
		}
		if err = json.Unmarshal(data, &fields); err != nil {
			return out, false, err
		}
		allowed := map[string]bool{}
		for _, field := range mutable {
			allowed[field] = true
		}
		for key := range fields {
			if !allowed[key] {
				delete(fields, key)
			}
		}
	}
	out, status, err := r.Request(WithRequestOptions(ctx, options), http.MethodPatch, path, nil, fields)
	return out, err == nil && status == http.StatusCreated, err
}

func mutableFields(kind string) []string {
	fields := "name labels status"
	switch kind {
	case "Account", "AgentIdentity":
		fields += " description"
	case "AccountMembership":
		fields += " role"
	case "Project":
		fields += " description target"
	case "AgentAuthorization":
		fields += " roles"
	case "Session", "AgentIdentityToken", "AccountInvitation":
		fields = "status"
	}
	return strings.Fields(fields)
}

func (c *Client) oauth(ctx context.Context, method, path string, form url.Values) (any, error) {
	var query url.Values
	var body any
	if method == http.MethodGet {
		query = form
	} else {
		body = form
	}
	data, status, headers, err := c.Exchange(ctx, method, path, query, body, true)
	if err != nil {
		return nil, err
	}
	if strings.Contains(headers.Get("Content-Type"), "text/html") || status >= 300 {
		return OAuthResponse{Body: string(data), Location: headers.Get("Location"), ContentType: headers.Get("Content-Type"), Status: status}, nil
	}
	if len(data) == 0 {
		return nil, nil
	}
	var result any
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode OAuth response: %w", err)
	}
	return result, nil
}

// Transition sends a lifecycle command with the version read by the caller.
func (r ResourceClient[T]) Transition(ctx context.Context, path string, value T, operation string) (T, error) {
	var zero T
	body, err := resource.Command(value, operation)
	if err != nil {
		return zero, err
	}
	options := Options(ctx)
	if options.IfMatch == "" {
		if meta, ok := any(&value).(interface{ Metadata() *model.Resource }); ok && meta.Metadata().Revision > 0 {
			options.IfMatch = fmt.Sprintf("\"%d\"", meta.Metadata().Revision)
		}
	}
	out, _, err := r.Request(WithRequestOptions(ctx, options), http.MethodPatch, path, nil, body)
	return out, err
}
