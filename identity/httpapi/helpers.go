package httpapi

import (
	"fmt"

	server "github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/pkg/bark"

	"github.com/gin-gonic/gin"
)

const (
	paginationLimit = 1024

	resourceValueKey = "resourceValueKey"

	idempotencyKeyHeader   = "Idempotency-Key"
	idempotencyKeyValueKey = "idempotencyKeyValue"
)

func RequireResourceTypeID[T ~string](ctx *gin.Context) T {
	id := bark.RequireResourceID(ctx)
	return T(id)
}

func RequirePathValue[T ~string](ctx *gin.Context, name string) T {
	return T(ctx.Param(name))
}

func RequireResource[T any](ctx *gin.Context) T {
	return ctx.MustGet(resourceValueKey).(T)
}

func IdempotencyAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		key := ctx.GetHeader(idempotencyKeyHeader)
		if key == "" {
			writeProblem(ctx, &server.Problem{Status: 400, Code: "idempotency-key-required", Detail: fmt.Sprintf("Missing %s header.", idempotencyKeyHeader)})
			return
		}

		ctx.Set(idempotencyKeyValueKey, key)
		ctx.Next()
	}
}

func RequireIdempotencyKey(ctx *gin.Context) string {
	return ctx.MustGet(idempotencyKeyValueKey).(string)
}
