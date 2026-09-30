package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func testClient(t *testing.T, handler roundTripFunc) *Client {
	t.Helper()
	c, err := New("https://example.test/prefix/v1/", Config{HTTPClient: &http.Client{Transport: handler}, Token: "identity", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestListQueryAndEscapedPath(t *testing.T) {
	selector, _ := manifest.ParseSelector("team in (a,b),!disabled")
	from := time.Date(2026, 9, 1, 1, 2, 3, 4, time.UTC)
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.EscapedPath() != "/prefix/v1/projects/p%2F%3F%23/memberships" {
			t.Fatalf("request %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer identity" {
			t.Fatal("missing bearer")
		}
		q := r.URL.Query()
		if q.Get("labels") != selector.String() || q.Get("name") != "space & plus+" || q.Get("cursor") != "c7" || q.Has("offset") || q.Get("limit") != "2" || q.Get("from") != from.Format(time.RFC3339Nano) || q.Get("till") != from.Add(time.Hour).Format(time.RFC3339Nano) {
			t.Fatalf("query: %v", q)
		}
		return reply(200, `{"items":[{"id":"m","name":"found"}],"total":9,"limit":2,"next":"c8"}`), nil
	})
	items, total, err := c.ProjectMemberships().List(context.Background(), "p/?#", manifest.SearchQuery{Selector: selector, Name: "space & plus+", Cursor: "c7", Limit: 2, FromTime: from, TillTime: from.Add(time.Hour)})
	if err != nil || *total.Total != 9 || total.Limit != 2 || total.Next != "c8" || len(items) != 1 || items[0].ID != "m" {
		t.Fatalf("list: %v %v %v", items, total, err)
	}
}

func TestMutationControls(t *testing.T) {
	var calls int
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch calls {
		case 1:
			if r.Method != "POST" || r.URL.Path != "/prefix/v1/accounts/a/projects" || r.Header.Get("Idempotency-Key") != "retry-key" || body["name"] != "project" {
				t.Fatalf("create: %s %s %v %v", r.Method, r.URL, r.Header, body)
			}
			return reply(201, `{"id":"p","revision":1,"name":"project"}`), nil
		default:
			if r.Method != "PATCH" || r.Header.Get("If-Match") != `"1"` || body["name"] != "changed" || r.Header.Get("X-Product-Control") != "on" {
				t.Fatalf("patch: %v %v", r.Header, body)
			}
			for _, key := range []string{"id", "revision", "created_at", "actor", "account_id", "current_context_id"} {
				if _, ok := body[key]; ok {
					t.Fatalf("immutable field %s", key)
				}
			}
			return reply(200, `{"id":"p","revision":2,"name":"changed"}`), nil
		}
	})
	ctx := context.Background()
	p, err := c.Projects().Create(WithRequestOptions(ctx, RequestOptions{IdempotencyKey: "retry-key"}), model.Project{Resource: model.Resource{AccountID: "a", Name: "project"}})
	if err != nil {
		t.Fatal(err)
	}
	p.Name = "changed"
	p, created, err := c.Projects().CreateOrUpdate(WithRequestOptions(ctx, RequestOptions{Header: http.Header{"X-Product-Control": {"on"}}}), p)
	if err != nil || created || p.Revision != 2 {
		t.Fatalf("update: %v %t %v", p, created, err)
	}
}

func TestPartialPatchAndErrors(t *testing.T) {
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		if string(data) != `{"status":"revoked"}` || r.Header.Get("If-Match") != `"3"` {
			t.Fatalf("patch %s %v", data, r.Header)
		}
		response := reply(412, `{"code":"precondition-failed","detail":"Stale revision","request_id":"request","fields":{"revision":"stale"}}`)
		response.Header.Set("Retry-After", "60")
		return response, nil
	})
	ctx := WithRequestOptions(context.Background(), RequestOptions{IfMatch: `"3"`, Patch: map[string]json.RawMessage{"status": json.RawMessage(`"revoked"`)}})
	_, _, err := c.Sessions().CreateOrUpdate(ctx, model.Session{Resource: model.Resource{ID: "session"}})
	var problem *Problem
	if !errors.As(err, &problem) || problem.Status != 412 || problem.Code != "precondition-failed" || problem.RequestID != "request" || problem.RetryAfter != "60" || problem.Fields["revision"] != "stale" {
		t.Fatalf("problem: %#v", err)
	}
}

func TestResponseSemantics(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		found     bool
		wantError bool
	}{
		{"found", 200, `{"id":"a"}`, true, false}, {"absent", 404, `{"code":"not-found"}`, false, false}, {"denied", 403, `{"detail":"Denied"}`, false, true}, {"malformed", 200, `broken`, false, true}, {"proxy", 502, `<html>bad gateway</html>`, false, true}, {"trailing", 200, `{} {}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t, func(*http.Request) (*http.Response, error) { return reply(tc.status, tc.body), nil })
			_, found, err := c.Accounts().Get(context.Background(), "a")
			if found != tc.found || (err != nil) != tc.wantError {
				t.Fatalf("found %t error %v", found, err)
			}
		})
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	c := testClient(t, func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	c.config.Timeout = time.Millisecond
	_, _, err := c.Principal().Get(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = c.Principal().Get(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestOAuthFormsAndBrowser(t *testing.T) {
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-Product-Control") != "" {
			t.Fatal("OAuth leaked resource credentials or controls")
		}
		if r.Method == "GET" {
			if r.URL.Query().Get("state") != "a+b" {
				t.Fatal(r.URL)
			}
			response := reply(200, "<html>authorize</html>")
			response.Header.Set("Content-Type", "text/html")
			return response, nil
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal(r.Header)
		}
		_ = r.ParseForm()
		if r.PostForm.Get("device_code") != "a+b & c" {
			t.Fatal(r.PostForm)
		}
		return reply(400, `{"error":"authorization_pending","error_description":"Wait for approval"}`), nil
	})
	ctx := WithRequestOptions(context.Background(), RequestOptions{Header: http.Header{"X-Product-Control": {"on"}}})
	_, err := c.OAuth().Token(ctx, url.Values{"device_code": {"a+b & c"}})
	var p *Problem
	if !errors.As(err, &p) || p.Code != "authorization_pending" {
		t.Fatal(err)
	}
	result, err := c.OAuth().Authorize(WithRequestOptions(context.Background(), RequestOptions{OAuthQuery: url.Values{"state": {"a+b"}}}))
	browser, ok := result.(OAuthResponse)
	if err != nil || !ok || browser.Body != "<html>authorize</html>" {
		t.Fatalf("%v %v", result, err)
	}
}

// The device grant waits out authorization_pending, slows down when asked, and
// returns the tokens once the person approves.
func TestDeviceGrantPollsUntilApproved(t *testing.T) {
	var polls int
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/prefix/oauth/device_authorization":
			if r.PostForm.Get("client_id") != "urthctl" || r.PostForm.Get("preferred_context") != "account" {
				t.Fatalf("device authorization form %v", r.PostForm)
			}
			return reply(200, `{"device_code":"dc","user_code":"UC","verification_uri":"https://example.test/oauth/device","expires_in":60,"interval":0}`), nil
		case "/prefix/oauth/token":
			polls++
			if r.PostForm.Get("grant_type") != deviceGrantType || r.PostForm.Get("device_code") != "dc" {
				t.Fatalf("token form %v", r.PostForm)
			}
			if polls == 1 {
				return reply(400, `{"error":"authorization_pending"}`), nil
			}
			return reply(200, `{"access_token":"at","refresh_token":"rt","scope":"account","account_id":"acme","expires_in":900}`), nil
		}
		t.Fatalf("unexpected %s", r.URL)
		return nil, nil
	})
	c.config.Timeout = 0
	device, err := c.StartDeviceAuthorization(context.Background(), "urthctl", "account")
	if err != nil || device.VerificationURIComplete != "https://example.test/oauth/device" {
		t.Fatalf("device: %+v %v", device, err)
	}
	device.Interval = 1
	start := time.Now()
	tokens, err := c.AwaitDeviceToken(context.Background(), "urthctl", device)
	if err != nil || tokens.AccessToken != "at" || tokens.AccountID != "acme" || polls != 2 {
		t.Fatalf("tokens: %+v %v after %d polls", tokens, err, polls)
	}
	if time.Since(start) < 2*time.Second {
		t.Fatal("polled faster than the server's interval")
	}
}

func TestDeviceGrantStopsAtADenial(t *testing.T) {
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		return reply(400, `{"error":"access_denied"}`), nil
	})
	_, err := c.AwaitDeviceToken(context.Background(), "urthctl", DeviceAuthorization{DeviceCode: "dc", ExpiresIn: 30, Interval: 1})
	var p *Problem
	if !errors.As(err, &p) || p.Code != "access_denied" {
		t.Fatal(err)
	}
}

func TestInvalidEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "://", "localhost:8080", "ftp://example.test", "https://user:secret@example.test", "https://example.test?q=a", "https://example.test/#x"} {
		if _, err := New(endpoint, Config{}); err == nil {
			t.Fatalf("accepted %q", endpoint)
		}
	}
}

func TestAllFollowsNextToTheLastPage(t *testing.T) {
	pages := map[string]string{
		"":   `{"items":[{"id":"a"},{"id":"b"}],"limit":2,"next":"c2"}`,
		"c2": `{"items":[{"id":"c"},{"id":"d"}],"limit":2,"next":"c3"}`,
		"c3": `{"items":[{"id":"e"}],"limit":2}`,
	}
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		body, ok := pages[r.URL.Query().Get("cursor")]
		if !ok || r.URL.Query().Has("offset") {
			t.Fatalf("unexpected page request %s", r.URL)
		}
		return reply(200, body), nil
	})
	items, err := Collect(context.Background(), manifest.SearchQuery{Limit: 2}, c.Sessions().List)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range items {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "a,b,c,d,e" {
		t.Fatalf("collected %v", ids)
	}
}

// An empty selector -- what an unset --selector parses to -- filters nothing
// and sends nothing.
func TestAnEmptySelectorSendsNoLabels(t *testing.T) {
	empty, err := manifest.ParseSelector("")
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Has("labels") {
			t.Errorf("sent %s", r.URL)
		}
		return reply(200, `{"items":[]}`), nil
	})
	if _, _, err := c.Sessions().List(context.Background(), manifest.SearchQuery{Selector: empty}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchQueriesRejectOffsetsBeforeSending(t *testing.T) {
	query := manifest.SearchQuery{Offset: 20, Limit: 20}
	c := testClient(t, func(r *http.Request) (*http.Response, error) {
		t.Errorf("offset query sent to server: %s", r.URL)
		return reply(200, `{"items":[],"limit":20}`), nil
	})
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"sessions":          func() error { _, _, err := c.Sessions().List(ctx, query); return err },
		"projects":          func() error { _, _, err := c.Projects().List(ctx, query); return err },
		"account projects":  func() error { _, _, err := c.Projects().ListForAccount(ctx, "account", query); return err },
		"member candidates": func() error { _, _, err := c.Directory().ProjectMemberCandidates(ctx, "p", "name", query); return err },
	} {
		err := call()
		var problem *Problem
		if !errors.As(err, &problem) || problem.Code != "offset-unsupported" {
			t.Errorf("%s: expected offset-unsupported problem, got %v", name, err)
		}
	}
}
