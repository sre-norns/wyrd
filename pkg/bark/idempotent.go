package bark

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/wyrd/pkg/idempotency"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const (
	// HTTPHeaderIdempotencyKey names a POST so that a retry of it is answered
	// from the record instead of executed again.
	HTTPHeaderIdempotencyKey = "Idempotency-Key"

	// HTTPHeaderIdempotentReplayed is set on a response replayed from a record.
	HTTPHeaderIdempotentReplayed = "Idempotent-Replayed"

	// MaxIdempotencyKeyLength is the longest key accepted, in bytes.
	MaxIdempotencyKeyLength = 200

	// maxIdempotentBody bounds the request body digested and held in memory.
	maxIdempotentBody = 8 << 20
)

var (
	// ErrIdempotencyKeyRequired is returned for a POST without a key on a route
	// that requires one.
	ErrIdempotencyKeyRequired error = manifest.NewStatusError(http.StatusBadRequest, "idempotency-key-required",
		"this request must carry an Idempotency-Key header")

	// ErrInvalidIdempotencyKey is returned for an empty or over-long key.
	ErrInvalidIdempotencyKey error = manifest.NewStatusError(http.StatusBadRequest, "invalid-idempotency-key",
		fmt.Sprintf("Idempotency-Key must be 1 to %d bytes", MaxIdempotencyKeyLength))

	// ErrIdempotencyConflict is returned when a key is reused for a different
	// request. The first request's outcome stands.
	ErrIdempotencyConflict error = manifest.NewStatusError(http.StatusConflict, "idempotency-conflict",
		"this Idempotency-Key was already used for a different request")

	// ErrIdempotencyInProgress is returned when the key's first request is still
	// executing. Retrying later gets its outcome.
	ErrIdempotencyInProgress error = manifest.NewStatusError(http.StatusConflict, "idempotency-in-progress",
		"a request with this Idempotency-Key is still being processed")
)

// IdempotencyOptions adjusts [Idempotent].
type IdempotencyOptions struct {
	// Required refuses a POST without a key. ADR 0001 §7 requires keys on
	// user-facing POSTs; a route idempotent by construction may leave it off.
	Required bool

	// Scope namespaces keys, typically by the authenticated principal, so that
	// two callers who happen to choose the same key do not see each other's
	// responses. Without it keys are global.
	Scope func(*gin.Context) string
}

// Idempotent returns middleware that makes POSTs safe to retry (ADR 0001 §7).
//
// A request carrying Idempotency-Key reserves the key before its handler runs,
// so two copies arriving together cannot both execute. When the handler
// finishes, a 2xx or 4xx response is recorded and replayed to any retry with the
// same key and body. A 5xx, or a panic, releases the key so the retry executes.
// The same key with a different method, path or body is refused.
//
// What it does not promise: that the record commits with the handler's own
// writes. The record is written after the handler returns, so a process that
// dies in between has applied the effect without recording it, and the retry,
// once the reservation's lease expires, executes again. Handlers whose effects
// must be exactly-once record inside their own transaction.
func Idempotent(store idempotency.Store, options IdempotencyOptions) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Request.Method != http.MethodPost {
			ctx.Next()
			return
		}

		key := ctx.GetHeader(HTTPHeaderIdempotencyKey)
		if key == "" {
			if options.Required {
				AbortWithProblem(ctx, http.StatusBadRequest, ErrIdempotencyKeyRequired)
				return
			}
			ctx.Next()
			return
		}
		if len(key) > MaxIdempotencyKeyLength {
			AbortWithProblem(ctx, http.StatusBadRequest, ErrInvalidIdempotencyKey)
			return
		}
		if options.Scope != nil {
			key = options.Scope(ctx) + "\x00" + key
		}

		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxIdempotentBody+1))
		if err != nil {
			AbortWithProblem(ctx, http.StatusBadRequest, fmt.Errorf("failed to read request body: %w", err))
			return
		}
		if len(body) > maxIdempotentBody {
			AbortWithProblem(ctx, http.StatusRequestEntityTooLarge, fmt.Errorf("request body exceeds %d bytes", maxIdempotentBody))
			return
		}
		ctx.Request.Body = io.NopCloser(bytes.NewReader(body))

		digest := requestDigest(ctx.Request.Method, ctx.Request.URL.Path, body)

		outcome, record, err := store.Reserve(ctx.Request.Context(), key, digest)
		if err != nil {
			AbortWithProblem(ctx, http.StatusServiceUnavailable, fmt.Errorf("idempotency store: %w", err))
			return
		}

		switch outcome {
		case idempotency.InProgress:
			ctx.Header("Retry-After", "1")
			AbortWithProblem(ctx, http.StatusConflict, ErrIdempotencyInProgress)
			return
		case idempotency.Recorded:
			if record.Digest != digest {
				AbortWithProblem(ctx, http.StatusConflict, ErrIdempotencyConflict)
				return
			}
			replay(ctx, record.Response)
			return
		}

		capture := &capturingWriter{ResponseWriter: ctx.Writer}
		ctx.Writer = capture

		completed := false
		defer func() {
			if completed {
				return
			}
			// A panic, or a response that is not an outcome: free the key so
			// the retry executes rather than waiting out the lease.
			_ = store.Release(ctx.Request.Context(), key)
		}()

		ctx.Next()

		status := capture.Status()
		if status >= http.StatusInternalServerError {
			return
		}
		// Whether or not the record is written, the key is not released: the
		// effect has happened. If recording failed, the reservation stands
		// until its lease expires, so a prompt retry is told the request is in
		// progress rather than executed a second time straight away.
		completed = true
		_ = store.Complete(ctx.Request.Context(), key, idempotency.Response{
			Status: status,
			Header: capture.Header().Clone(),
			Body:   capture.body.Bytes(),
		})
	}
}

func requestDigest(method, path string, body []byte) string {
	h := sha256.New()
	_, _ = io.WriteString(h, method+" "+path+"\n")
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func replay(ctx *gin.Context, response idempotency.Response) {
	for name, values := range response.Header {
		for _, value := range values {
			ctx.Writer.Header().Add(name, value)
		}
	}
	ctx.Header(HTTPHeaderIdempotentReplayed, strconv.FormatBool(true))
	ctx.Abort()
	ctx.Status(response.Status)
	_, _ = ctx.Writer.Write(response.Body)
}

// capturingWriter keeps a copy of the body written through it.
type capturingWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *capturingWriter) Write(data []byte) (int, error) {
	w.body.Write(data)
	return w.ResponseWriter.Write(data)
}

func (w *capturingWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}
