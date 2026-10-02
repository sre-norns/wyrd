package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/sre-norns/wyrd/pkg/manifest"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	server "github.com/sre-norns/wyrd/identity"
	expbench "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/pkg/bark"
)

func writeProblem(ctx *gin.Context, err error) {
	var domain *server.Problem
	var input *resource.InputError
	switch {
	case errors.As(err, &input):
		err = &bark.Problem{Status: 422, Code: "validation", Title: "Validation failed", Detail: input.Detail, Fields: map[string]string{input.Field: input.Detail}}
	case errors.As(err, &domain):
		fields := map[string]string{}
		for key, value := range domain.Fields {
			if !ctx.GetBool("identityCommandInput") {
				value, _ := ctx.Get(resourceValueKey)
				key = resource.FieldPathFor(value, key)
			}
			fields[key] = value
		}
		err = &bark.Problem{Type: domain.Type, Status: domain.Status, Title: http.StatusText(domain.Status), Code: domain.Code, Detail: domain.Detail, Fields: fields}
		if domain.Status == 429 {
			ctx.Header("Retry-After", "60")
		}
	}
	bark.AbortResourceError(ctx, err)
}

type resourceResponse[T any] struct{ ctx *gin.Context }

func response[T any](ctx *gin.Context) *resourceResponse[T] { return &resourceResponse[T]{ctx} }
func (r *resourceResponse[T]) send(status int, v T) {
	if m, ok := any(&v).(interface{ Metadata() *expbench.Resource }); ok {
		meta := m.Metadata()
		r.ctx.Header("ETag", server.ETag(meta.Revision))
		if status == 201 && meta.ID != "" {
			name := reflect.TypeOf(v).Name()
			path := resourcePaths[name]
			if path != "" {
				r.ctx.Header("Location", "/v1/"+path+"/"+meta.ID)
			}
		}
	}
	if m, ok := any(&v).(interface {
		SystemMetadata() *expbench.SystemRecord
	}); ok {
		r.ctx.Header("ETag", server.ETag(m.SystemMetadata().Revision))
	}
	if owner := resource.OutcomeOwner(v); owner != "" {
		r.ctx.Set("identityOutcomeAccount", owner)
	}
	var body any
	var err error
	if r.ctx.Request.Method == http.MethodGet {
		body, err = resource.Encode(v)
	} else {
		body, err = resource.EncodeResult(v)
	}
	if err != nil {
		writeProblem(r.ctx, err)
		return
	}
	r.ctx.JSON(status, body)
}
func (r *resourceResponse[T]) Found(v T, found bool, err error) {
	if err != nil {
		writeProblem(r.ctx, err)
		return
	}
	if !found {
		writeProblem(r.ctx, &server.Problem{Status: 404, Code: "not-found", Detail: "Resource not found."})
		return
	}
	r.send(200, v)
}
func (r *resourceResponse[T]) Created(v T, err error) {
	if err != nil {
		writeProblem(r.ctx, err)
		return
	}
	r.send(201, v)
}
func (r *resourceResponse[T]) CreatedOrUpdated(v T, created bool, err error) {
	if err != nil {
		writeProblem(r.ctx, err)
		return
	}

	status := 200
	if created {
		status = 201
	}
	r.send(status, v)
}
func (r *resourceResponse[T]) List(items []T, page manifest.Page, err error) {
	if err != nil {
		writeProblem(r.ctx, err)
		return
	}
	body, encodeErr := resource.Encode(listPage(items, page))
	if encodeErr != nil {
		writeProblem(r.ctx, encodeErr)
		return
	}
	r.ctx.JSON(200, body)
}

// listPage is the list contract (ADR 0001 §8): {items, limit, next?, total?}.
func listPage[T any](items []T, page manifest.Page) gin.H {
	if items == nil {
		items = []T{}
	}
	body := gin.H{"items": items, "limit": page.Limit}
	if page.Next != "" {
		body["next"] = page.Next
	}
	if page.Total != nil {
		body["total"] = *page.Total
	}
	return body
}

var resourcePaths = map[string]string{"Account": "accounts", "AccountMembership": "account-memberships", "AccountInvitation": "account-invitations", "AgentIdentity": "agent-identities", "AgentIdentityToken": "agent-identity-tokens", "Project": "projects", "ProjectMembership": "project-memberships", "AgentAuthorization": "agent-authorizations", "Session": "sessions"}

func ResourceValueAPI[T any]() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var value T
		ctx.Set("identityCommandInput", !resource.IsResource(value))
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1048577))
		if err != nil || len(body) > 1048576 {
			writeProblem(ctx, &server.Problem{Status: 413, Code: "body-too-large", Detail: "The request exceeds 1 MiB."})
			return
		}
		var fields map[string]json.RawMessage
		if resource.IsResource(value) {
			fields, err = resource.DecodeInput(body, &value, ctx.Request.Method == http.MethodPatch)
		} else {
			d := json.NewDecoder(bytes.NewReader(body))
			d.DisallowUnknownFields()
			err = d.Decode(&value)
			if err != nil {
				err = &server.Problem{Status: 400, Code: "invalid-json", Detail: "Invalid command body."}
			}
			if err == nil && d.Decode(new(any)) != io.EOF {
				err = &resource.InputError{Field: "body", Detail: "Only one JSON value is permitted."}
			}
		}
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		if m, ok := any(&value).(interface{ Metadata() *expbench.Resource }); ok {
			if ctx.Request.Method == http.MethodPatch {
				m.Metadata().ID = ctx.Param("id")
			}
			if _, ok := any(value).(expbench.Project); ok && ctx.Request.Method == http.MethodPost {
				m.Metadata().AccountID = expbench.AccountID(ctx.Param("id"))
			}
		}

		req := server.Request{ID: ctx.GetString("requestID"), Method: ctx.Request.Method, Target: ctx.Request.URL.Path, IfMatch: ctx.GetHeader("If-Match"), Patch: fields, InvitationToken: ctx.GetHeader("X-Invitation-Token")}
		req.SupportReason, req.SupportReference = supportAuditFields(body)
		ctx.Request = ctx.Request.WithContext(server.WithRequest(ctx.Request.Context(), req))
		ctx.Set(resourceValueKey, value)
		ctx.Next()
	}
}

type capturedWriter struct {
	gin.ResponseWriter
	body    bytes.Buffer
	status  int
	headers http.Header
}

func (w *capturedWriter) Header() http.Header    { return w.headers }
func (w *capturedWriter) WriteHeader(status int) { w.status = status }
func (w *capturedWriter) WriteHeaderNow() {
	if w.status == 0 {
		w.status = 200
	}
}
func (w *capturedWriter) Write(b []byte) (int, error)       { w.WriteHeaderNow(); return w.body.Write(b) }
func (w *capturedWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *capturedWriter) Status() int {
	if w.status == 0 {
		return 200
	}
	return w.status
}
func (w *capturedWriter) Size() int     { return w.body.Len() }
func (w *capturedWriter) Written() bool { return w.status != 0 || w.body.Len() > 0 }
func authenticated(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id := uuid.NewString()
		ctx.Set("requestID", id)
		ctx.Header("X-Request-ID", id)
		token, ok := strings.CutPrefix(ctx.GetHeader("Authorization"), "Bearer ")
		if !ok {
			writeProblem(ctx, &server.Problem{Status: 401, Code: "unauthenticated", Detail: "A bearer credential is required."})
			return
		}
		p, err := s.Authenticate(ctx.Request.Context(), token)
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		req := server.Request{Sort: ctx.Query("sort"), Direction: ctx.Query("direction"), ID: id, Method: ctx.Request.Method, Target: ctx.Request.URL.Path, IfMatch: ctx.GetHeader("If-Match"), InvitationToken: ctx.GetHeader("X-Invitation-Token")}
		ctx.Request = ctx.Request.WithContext(server.WithPrincipal(s.RequestContext(ctx.Request.Context(), req), p))
		if err := s.AuthorizeRoute(ctx.Request.Context(), ctx.Request.URL.Path); err != nil {
			if auditErr := s.AuditRejected(ctx.Request.Context(), err); auditErr != nil {
				err = auditErr
			}
			writeProblem(ctx, err)
			return
		}
		if err := s.AllowRequest(ctx.Request.Context()); err != nil {
			if auditErr := s.AuditRejected(ctx.Request.Context(), err); auditErr != nil {
				err = auditErr
			}
			writeProblem(ctx, err)
			return
		}
		if ctx.Request.Method == http.MethodGet {
			ctx.Next()
			return
		}
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1048577))
		if err != nil || len(body) > 1048576 {
			writeProblem(ctx, &server.Problem{Status: 413, Code: "body-too-large", Detail: "The request exceeds 1 MiB."})
			return
		}
		req.SupportReason, req.SupportReference = supportAuditFields(body)
		ctx.Request = ctx.Request.WithContext(server.WithRequest(ctx.Request.Context(), req))
		ctx.Request.Body = io.NopCloser(bytes.NewReader(body))
		normalized := body
		var parsed any
		if json.Unmarshal(body, &parsed) == nil {
			normalized, _ = json.Marshal(parsed)
		}
		real := ctx.Writer
		out, err := s.TransactHTTP(ctx.Request.Context(), token, ctx.GetHeader("Idempotency-Key"), string(normalized), func(tx context.Context) server.HTTPOutcome {
			capture := &capturedWriter{ResponseWriter: real, headers: real.Header().Clone()}
			ctx.Writer = capture
			ctx.Request = ctx.Request.WithContext(tx)
			ctx.Next()
			return server.HTTPOutcome{Status: capture.Status(), Body: capture.body.Bytes(), Header: capture.headers, AccountID: expbench.AccountID(ctx.GetString("identityOutcomeAccount"))}
		})
		ctx.Writer = real
		if err != nil {
			writeProblem(ctx, err)
			return
		}
		for k, vs := range out.Header {
			real.Header()[k] = vs
		}
		if out.Status >= 400 {
			var failure server.Problem
			_ = json.Unmarshal(out.Body, &failure)
			if err := s.AuditRejected(server.WithPrincipal(s.RequestContext(context.Background(), req), p), &failure); err != nil {
				writeProblem(ctx, err)
				return
			}
		}
		ctx.Data(out.Status, out.Header.Get("Content-Type"), out.Body)
		ctx.Abort()
	}
}

func oauthResponse(ctx *gin.Context, result any, err error) {
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("Pragma", "no-cache")
	if err != nil {
		var p *server.Problem
		if errors.As(err, &p) {
			ctx.JSON(p.Status, gin.H{"error": p.Code, "error_description": p.Detail})
		} else {
			ctx.JSON(500, gin.H{"error": "server_error"})
		}
		return
	}
	switch v := result.(type) {
	case server.BrowserAuthorization:
		renderOAuthPage(ctx, 200, "login", oauthPage{BrowserAuthorization: v, Title: "Authorize access", Device: ctx.Request.URL.Path == "/oauth/device" || v.UserCode != ""})
	case server.AuthorizationRedirect:
		ctx.Redirect(302, v.URL)
	default:
		ctx.JSON(200, result)
	}
}
func browserApproval(s *server.Service) gin.HandlerFunc {
	return browserAuthorization(s, false)
}

func browserLogin(s *server.Service) gin.HandlerFunc {
	return browserAuthorization(s, true)
}

func browserAuthorization(s *server.Service, webLogin bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := ctx.Request.ParseForm(); err != nil {
			oauthResponse(ctx, nil, err)
			return
		}
		if webLogin && (ctx.Request.Form.Get("client_id") != s.WebClientID() || ctx.Request.Form.Get("user_code") != "") {
			renderOAuthPage(ctx, http.StatusBadRequest, "complete", oauthPage{WebLogin: true, RetryLabel: "Back to login", Title: "Login unavailable", Message: "Start sign-in from the web application.", Failed: true, Retry: "/sign-in"})
			return
		}
		if ctx.Request.Method == http.MethodGet {
			ctx.Request.Form.Del("password")
			ctx.Request.Form.Del("email")
		}
		authorizeBrowser(ctx, s, webLogin, ctx.Request.URL.Path == "/oauth/device", ctx.Request.Form, "")
	}
}

// authorizeBrowser runs one browser authorization step and renders the result.
// A provider callback resumes the flow here with an internal authentication
// proof; action then names the page that the next form posts to.
func authorizeBrowser(ctx *gin.Context, s *server.Service, webLogin, device bool, form url.Values, action string) {
	if webLogin {
		form.Set("decision", "approve")
	}
	result, err := s.OAuth().Authorize(s.RequestContext(ctx.Request.Context(), server.Request{OAuthQuery: form, DeviceFlow: device}))
	if err != nil {
		status := http.StatusInternalServerError
		message := "The service cannot complete authorization. Try again."
		var problem *server.Problem
		if errors.As(err, &problem) {
			status = problem.Status
			message = problem.Detail
		}
		if webLogin {
			// Validate the transaction again without credentials before redisplaying it.
			retryForm := url.Values{}
			for key, values := range form {
				retryForm[key] = append([]string(nil), values...)
			}
			for _, key := range []string{"password", "email", "auth_ticket"} {
				retryForm.Del(key)
			}
			retry, retryErr := s.OAuth().Authorize(s.RequestContext(ctx.Request.Context(), server.Request{OAuthQuery: retryForm}))
			if page, ok := retry.(server.BrowserAuthorization); retryErr == nil && ok {
				page.Email = form.Get("email")
				if problem != nil && problem.Code == "access_denied" {
					message = "Login failed. Check your email and password, and confirm that you have access to this workspace."
					if form.Get("auth_ticket") != "" {
						message = "Login failed. Confirm that your user is active and has access to a workspace, then sign in again."
					}
				}
				renderOAuthPage(ctx, status, "web-login", oauthPage{BrowserAuthorization: page, Title: "Log in", WebLogin: true, Failed: true, Message: message})
				return
			}
		}
		if device && problem != nil && (problem.Code == "invalid_grant" || problem.Code == "expired_token") {
			message = deviceRestart(ctx, "The device request expired or is complete.")
		}
		renderOAuthPage(ctx, status, "complete", oauthPage{WebLogin: webLogin, Title: "Authorization could not complete", Message: message, Failed: true, Retry: oauthRetryFor(authorizationPath(webLogin, device), form)})
		return
	}
	if result, ok := result.(map[string]string); ok && result["status"] == "approved" {
		page := oauthPage{Title: strings.TrimSuffix(presentation(ctx).Copy.OrDefault().Headline, "."), Message: "You can now close the window. Return to your terminal to continue."}
		if form.Get("decision") == "deny" {
			page.Title = "Access denied"
			page.Message = "The application has no access. You can now close the window."
			page.Failed = true
		}
		renderOAuthPage(ctx, 200, "complete", page)
		return
	}
	if page, ok := result.(server.BrowserAuthorization); ok {
		if webLogin {
			renderOAuthPage(ctx, 200, "web-login", oauthPage{BrowserAuthorization: page, Title: "Log in", WebLogin: true, AccountArchived: form.Get("notice") == "account-archived", Action: action})
			return
		}
		renderOAuthPage(ctx, 200, "login", oauthPage{BrowserAuthorization: page, Title: "Authorize access", Device: device || page.UserCode != "", Action: action})
		return
	}
	oauthResponse(ctx, result, nil)
}

func authorizationPath(webLogin, device bool) string {
	switch {
	case webLogin:
		return "/oauth/login"
	case device:
		return "/oauth/device"
	}
	return "/oauth/authorize"
}

// searchable reads a list query. Lists page by cursor only: offset, page and
// pageSize are refused rather than ignored, since a client whose offset is
// ignored is served the first page forever.
func searchable() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := refusePositionalPaging(ctx); err != nil {
			writeProblem(ctx, err)
			return
		}
		var params bark.SearchParams
		if err := ctx.ShouldBindQuery(&params); err != nil {
			writeProblem(ctx, &server.Problem{Status: 400, Code: "invalid-query", Detail: err.Error()})
			return
		}
		query, err := params.BuildCursorQuery(bark.DefaultPageLimits)
		if err != nil {
			writeProblem(ctx, &server.Problem{Status: 400, Code: "invalid-query", Detail: err.Error()})
			return
		}
		ctx.Set("serviceSearchQuery", query)
		ctx.Next()
	}
}

// refusePositionalPaging answers a request that pages by position.
func refusePositionalPaging(ctx *gin.Context) error {
	for _, name := range []string{"offset", "page", "pageSize"} {
		if _, ok := ctx.GetQuery(name); ok {
			return &server.Problem{Status: 400, Code: "offset-unsupported", Detail: "Lists page by cursor only: pass the previous page's next as cursor."}
		}
	}
	return nil
}
func requireSearchQuery(ctx *gin.Context) manifest.SearchQuery {
	return ctx.MustGet("serviceSearchQuery").(manifest.SearchQuery)
}

func authenticationRate(s *server.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if strings.HasPrefix(ctx.Request.URL.Path, "/oauth/") {
			// no-referrer pages can send a null Origin. Fetch Metadata still
			// identifies same-origin browser submissions without exposing link tokens.
			sameOriginForm := ctx.GetHeader("Origin") == "null" && ctx.GetHeader("Sec-Fetch-Site") == "same-origin"
			if ctx.Request.Method == http.MethodPost && !s.AuthenticationOriginAllowed(ctx.GetHeader("Origin")) && !sameOriginForm {
				renderOAuthPage(ctx, 403, "complete", oauthPage{Title: "Request blocked", Message: "Start this request from Exp-Bench.", Failed: true, Retry: "/sign-in"})
				ctx.Abort()
				return
			}
			if err := s.AllowAuthentication(ctx.Request.Context(), ctx.ClientIP()); err != nil {
				oauthResponse(ctx, nil, err)
				ctx.Abort()
				return
			}
			ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 16384)
		}
		ctx.Next()
	}
}

// Record only bounded support fields. Never copy mutation payloads into system audit.
func supportAuditFields(body []byte) (string, string) {
	var fields struct {
		Reason    string `json:"reason"`
		Reference string `json:"reference"`
	}
	if json.Unmarshal(body, &fields) != nil {
		return "", ""
	}
	if len(fields.Reason) > 2000 {
		fields.Reason = ""
	}
	if len(fields.Reference) > 500 {
		fields.Reference = ""
	}
	return fields.Reason, fields.Reference
}

// SendResource lets product-mounted identity/system routes use the same codec,
// ETag and replay ownership as Mount. It does not authorize the operation.
func SendResource[T any](ctx *gin.Context, status int, value T) { response[T](ctx).send(status, value) }
