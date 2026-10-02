package bark

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// MimeTypeProblemJSON is the media type of an error response (RFC 9457).
const MimeTypeProblemJSON = "application/problem+json"

// Problem is an error response in the RFC 9457 shape, with a stable Code a
// client branches on (ADR 0001 §6). Detail may change between releases; Code
// does not.
type Problem struct {
	Type      string            `json:"type,omitempty"`
	Title     string            `json:"title"`
	Status    int               `json:"status"`
	Code      string            `json:"code"`
	Detail    string            `json:"detail,omitempty"`
	Instance  string            `json:"instance,omitempty"`
	RequestID string            `json:"requestId,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// Error implements error, so a Problem can be returned where errors are.
func (p *Problem) Error() string {
	if p.Detail != "" {
		return p.Detail
	}
	return p.Title
}

// HTTPStatus implements the status-reporting interface [ProblemFromError] reads.
func (p *Problem) HTTPStatus() int { return p.Status }

// ProblemCode implements the status-reporting interface [ProblemFromError] reads.
func (p *Problem) ProblemCode() string { return p.Code }

// statusReporter is implemented by errors that say how they should be reported:
// [manifest.StatusError], [Problem] and [ErrorResponse].
type statusReporter interface {
	HTTPStatus() int
	ProblemCode() string
}

// HTTPStatus lets the pre-existing ErrorResponse sentinels map to problems.
func (e *ErrorResponse) HTTPStatus() int { return e.Code }

// ProblemCode derives a code from the status for ErrorResponse sentinels, which
// predate codes.
func (e *ErrorResponse) ProblemCode() string { return codeForStatus(e.Code) }

// ProblemFromError builds the problem an error should be reported as.
//
// An error that says how to report itself -- a [manifest.StatusError], anywhere
// in its chain -- supplies the status and code, and its full message becomes the
// detail. Any other error is reported with fallbackStatus.
func ProblemFromError(fallbackStatus int, err error) *Problem {
	if err == nil {
		return nil
	}

	// A copy: the caller fills in per-request fields, and the Problem found may
	// be a sentinel shared by every request.
	var already *Problem
	if errors.As(err, &already) {
		problem := *already
		return &problem
	}

	status, code := fallbackStatus, codeForStatus(fallbackStatus)
	var reporter statusReporter
	if errors.As(err, &reporter) {
		status, code = reporter.HTTPStatus(), reporter.ProblemCode()
	}

	return &Problem{
		Title:  http.StatusText(status),
		Status: status,
		Code:   code,
		Detail: err.Error(),
	}
}

// AbortWithProblem ends the request with err as an application/problem+json
// response. See [ProblemFromError] for how the status and code are chosen.
func AbortWithProblem(ctx *gin.Context, fallbackStatus int, err error) {
	problem := ProblemFromError(fallbackStatus, err)
	if problem == nil {
		ctx.AbortWithStatus(fallbackStatus)
		return
	}
	if problem.Instance == "" && ctx.Request != nil && ctx.Request.URL != nil {
		problem.Instance = ctx.Request.URL.Path
	}

	// gin's JSON rendering keeps a Content-Type that is already set.
	ctx.Header(HTTPHeaderContentType, MimeTypeProblemJSON)
	ctx.AbortWithStatusJSON(problem.Status, problem)
}

// codeForStatus is the code of an error that carries none: the status text,
// kebab-cased ("Not Found" -> "not-found").
func codeForStatus(status int) string {
	text := http.StatusText(status)
	if text == "" {
		return "error"
	}
	return strings.ReplaceAll(strings.ToLower(text), " ", "-")
}

// AbortResourceError reports a resource API failure with its declared status and
// code. Unexpected failures are safe generic 500s. Unlike legacy response
// helpers, it never turns a typed 403/404/412 into the fallback 400 or exposes an
// internal error chain. Services adapt their domain errors to Problem first to
// retain field errors. OAuth endpoints keep their own protocol error handler.
func AbortResourceError(ctx *gin.Context, err error) {
	if err == nil {
		return
	}
	p := ProblemFromError(http.StatusInternalServerError, err)
	// A wrapper can contain internal context. Only a declared public error's
	// own message belongs in the response, not the wrapping error chain.
	var explicit *Problem
	var declared interface {
		statusReporter
		error
	}
	if !errors.As(err, &explicit) && errors.As(err, &declared) {
		p.Detail = declared.Error()
	}
	if p.Status < 400 || p.Status > 599 {
		p = &Problem{Status: http.StatusInternalServerError, Code: "internal-error"}
	}
	if p.Status >= 500 {
		status, code := p.Status, p.Code
		if status == http.StatusInternalServerError {
			code = "internal-error"
		}
		p = &Problem{Status: status, Code: code, Title: http.StatusText(status), Detail: "The service cannot complete the request."}
	}
	if p.RequestID == "" {
		p.RequestID = ctx.GetString("requestID")
	}
	AbortWithProblem(ctx, http.StatusInternalServerError, p)
}
