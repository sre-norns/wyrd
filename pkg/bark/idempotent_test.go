package bark

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/idempotency"
	"github.com/stretchr/testify/require"
)

type idempotentFixture struct {
	router *gin.Engine
	calls  atomic.Int32
	status atomic.Int32
	gate   chan struct{}
}

func newIdempotentFixture(options IdempotencyOptions) *idempotentFixture {
	gin.SetMode(gin.TestMode)
	f := &idempotentFixture{router: gin.New()}
	f.status.Store(http.StatusCreated)

	f.router.Use(Idempotent(idempotency.NewMemoryStore(time.Minute), options))
	f.router.POST("/runs", func(ctx *gin.Context) {
		n := f.calls.Add(1)
		if f.gate != nil {
			<-f.gate
		}
		ctx.Header("X-Run", "run-"+string(rune('0'+n)))
		ctx.String(int(f.status.Load()), "created run %d", n)
	})
	f.router.GET("/runs", func(ctx *gin.Context) { ctx.String(http.StatusOK, "list") })
	return f
}

func (f *idempotentFixture) post(path, key, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set(HTTPHeaderIdempotencyKey, key)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func TestIdempotentReplaysTheFirstOutcome(t *testing.T) {
	f := newIdempotentFixture(IdempotencyOptions{})

	first := f.post("/runs", "k1", `{"a":1}`)
	require.Equal(t, http.StatusCreated, first.Code)
	require.Equal(t, "created run 1", first.Body.String())

	retry := f.post("/runs", "k1", `{"a":1}`)
	require.Equal(t, http.StatusCreated, retry.Code)
	require.Equal(t, "created run 1", retry.Body.String(), "the retry gets the first response, not a second run")
	require.Equal(t, "run-1", retry.Header().Get("X-Run"))
	require.Equal(t, "true", retry.Header().Get(HTTPHeaderIdempotentReplayed))
	require.EqualValues(t, 1, f.calls.Load())

	// No key: nothing is recorded and every request executes.
	f.post("/runs", "", `{}`)
	f.post("/runs", "", `{}`)
	require.EqualValues(t, 3, f.calls.Load())
}

func TestIdempotentRefusesAKeyReusedForAnotherRequest(t *testing.T) {
	f := newIdempotentFixture(IdempotencyOptions{})
	require.Equal(t, http.StatusCreated, f.post("/runs", "k1", `{"a":1}`).Code)

	other := f.post("/runs", "k1", `{"a":2}`)
	require.Equal(t, http.StatusConflict, other.Code)
	require.Contains(t, other.Body.String(), `"code":"idempotency-conflict"`)
	require.EqualValues(t, 1, f.calls.Load())
}

// Two copies of a request arriving together: the second must not execute while
// the first is still running.
func TestIdempotentRefusesAConcurrentDuplicate(t *testing.T) {
	f := newIdempotentFixture(IdempotencyOptions{})
	f.gate = make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	var first *httptest.ResponseRecorder
	go func() {
		defer wg.Done()
		first = f.post("/runs", "k1", `{}`)
	}()
	require.Eventually(t, func() bool { return f.calls.Load() == 1 }, time.Second, time.Millisecond)

	duplicate := f.post("/runs", "k1", `{}`)
	require.Equal(t, http.StatusConflict, duplicate.Code)
	require.Contains(t, duplicate.Body.String(), `"code":"idempotency-in-progress"`)
	require.Equal(t, "1", duplicate.Header().Get("Retry-After"))

	close(f.gate)
	wg.Wait()
	require.Equal(t, http.StatusCreated, first.Code)
	require.EqualValues(t, 1, f.calls.Load())
}

func TestIdempotentReleasesTheKeyOnServerErrorAndPanic(t *testing.T) {
	f := newIdempotentFixture(IdempotencyOptions{})

	f.status.Store(http.StatusServiceUnavailable)
	require.Equal(t, http.StatusServiceUnavailable, f.post("/runs", "k1", `{}`).Code)
	f.status.Store(http.StatusCreated)
	retry := f.post("/runs", "k1", `{}`)
	require.Equal(t, http.StatusCreated, retry.Code, "a 5xx is not an outcome; the retry executes")
	require.EqualValues(t, 2, f.calls.Load())

	recovering := gin.New()
	recovering.Use(gin.Recovery(), Idempotent(idempotency.NewMemoryStore(time.Minute), IdempotencyOptions{}))
	var calls atomic.Int32
	recovering.POST("/panics", func(ctx *gin.Context) {
		if calls.Add(1) == 1 {
			panic("handler failed")
		}
		ctx.String(http.StatusOK, "ok")
	})
	send := func() int {
		req := httptest.NewRequest(http.MethodPost, "/panics", strings.NewReader(`{}`))
		req.Header.Set(HTTPHeaderIdempotencyKey, "p1")
		w := httptest.NewRecorder()
		recovering.ServeHTTP(w, req)
		return w.Code
	}
	require.Equal(t, http.StatusInternalServerError, send())
	require.Equal(t, http.StatusOK, send(), "a panicked request must not hold its key until the lease expires")
}

func TestIdempotentKeyRules(t *testing.T) {
	required := newIdempotentFixture(IdempotencyOptions{Required: true})
	missing := required.post("/runs", "", `{}`)
	require.Equal(t, http.StatusBadRequest, missing.Code)
	require.Contains(t, missing.Body.String(), `"code":"idempotency-key-required"`)

	long := required.post("/runs", strings.Repeat("k", MaxIdempotencyKeyLength+1), `{}`)
	require.Equal(t, http.StatusBadRequest, long.Code)
	require.Contains(t, long.Body.String(), `"code":"invalid-idempotency-key"`)

	// Only POST is subject to it.
	w := httptest.NewRecorder()
	required.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/runs", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.EqualValues(t, 0, required.calls.Load())
}

// Two callers choosing the same key must not receive each other's responses.
func TestIdempotentScopesKeysPerCaller(t *testing.T) {
	f := newIdempotentFixture(IdempotencyOptions{Scope: func(ctx *gin.Context) string { return ctx.GetHeader("X-Principal") }})

	alice := f.post("/runs", "shared", `{}`, "X-Principal", "alice")
	bob := f.post("/runs", "shared", `{}`, "X-Principal", "bob")
	require.Equal(t, "created run 1", alice.Body.String())
	require.Equal(t, "created run 2", bob.Body.String())
	require.Empty(t, bob.Header().Get(HTTPHeaderIdempotentReplayed))
}
