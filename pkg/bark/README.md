# SRE-Norns: Wyrd/Bark! 
Reusable REST API components that you need to extend (Gin-gonic)[github.com/gin-gonic/gin]!

This go-module provides a number types, helpers and filters that are very handy when creating rich Rest API using popular Go web framework.

# Usage
Using search filters allows to have standard API with filter labels and pagination:
```go
    // Define API that allows filters and returns paginated response
    api.GET("/artifacts", bark.SearchableAPI(paginationLimit), ..., func(ctx *gin.Context) {
        // Extract search query from the context:
        searchQuery := bark.RequireSearchQuery(ctx)

        // Use the query in your API;
        results, err := service.GetArtifactsApi().List(ctx.Request.Context(), searchQuery)
        ... 
    })
```

# Middleware
The library offers a collection of middleware / filter that streamline implementation of a service responsible for a collection of resource. 

- `ContentTypeAPI` - enables API to support different serialization formats, respecting [`Accept`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept) HTTP Request headers.
- `SearchableAPI` - enables APIs to support query filter and pagination.
- `AuthBearerAPI` - enables APIs to read Auth Bearer token.
- `ResourceAPI` - streamline implementation of APIs that serves a single resource.
- `VersionedResourceAPI` - enables API implementation that can support request to versioned resources.
- `ManifestAPI` - helps to streamline implementation of APIs that detail with `manifest.Resource`



### Middleware: `ContentTypeAPI`
This function returns `gin.HandlerFunc` as middleware to add support for response marshaler selection based on [`Accept`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept) HTTP Request header.

It is expected to be used in conjunction with `bark.MarshalResponse` and `bark.ReplyResourceCreated` method that select recommended marshaler method to be used for http response.

For example, a client can call `/artifacts/:id` API with `Accept` header to inform the server what response type is expected back. Given valid request, for example:

```
GET /artifacts/:id HTTP/1.1
Host: localhost:8080
Accept: application/xml
```

The server would select XML as serialization method:
```
HTTP/1.1 200 OK
Content-Type: application/xml; charset=utf-8

....
```


#### Usage: 
```go
    api.GET("/artifacts/:id", bark.ContentTypeAPI(), func(ctx *gin.Context) {
        ...

        bark.MarshalResponse(ctx, http.StatusOK, resource) // NOTE: To use this method ContentTypeAPI middleware is required
    })

    v1.POST("/artifacts", bark.ContentTypeAPI(), func(ctx *gin.Context) {
        ...
        
        bark.ReplyResourceCreated(ctx, result.ID, result) // NOTE: To use this method ContentTypeAPI middleware is required
    })

``` 


### Middleware: `SearchableAPI`
This function returns `gin.HandlerFunc` as middleware to add support pagination query parameters and  `labels` query parameters.
The idea is that Rest APIs that returns a collection of results should support pagination and ability to narrows down a dataset returned.

Typical implementation takes shape of query parameters such as `filter` and `page` numbers.

Using `SearchableAPI()` gives users ability to call `RequireSearchQuery` to get an object representing search query passed by clients.
Note that `RequireSearchQuery` will `panic` if there was not `SearchableAPI` call in the filter chain to add query object to the context.


#### Usage: 
Assuming the server has `/artifacts` REST endpoint that is meant to return a collection of artifacts / objects. With `SearchableAPI` middleware, clients can use `labels` and `page` query parameters to control pagination and filter.

```
GET /artifacts?labels=key&page=3 HTTP/1.1
```

To support the above query parameters, following code should be added to the handler:
```go
    api.GET("/artifacts", bark.SearchableAPI(), func(ctx *gin.Context) {
        ...
        searchQuery := bark.RequireSearchQuery(ctx) 

        results := myservice.Find(ctx, searchQuery)
        ...

    })
```

### Middleware: `SearchableAPIWithLimits`
`SearchableAPI(n)` uses `n` as both the default page size and the cap.
`SearchableAPIWithLimits(bark.DefaultPageLimits)` sets them apart (100 and 1024)
and also accepts `offset`, `limit`, `cursor` and `fields`; `page`/`pageSize`
remain aliases, and `offset`/`limit` win when both are given.

Reply with a page in the `{items, limit, next, total}` shape:

```go
    api.GET("/scenarios", bark.SearchableAPIWithLimits(bark.DefaultPageLimits), func(ctx *gin.Context) {
        var items []Scenario
        page, err := store.FindPage(ctx, &items, bark.RequireSearchQuery(ctx))
        bark.WithContext[Scenario](ctx).Page(items, page, err)
    })
```

### Errors: `AbortWithProblem`
Renders any error as `application/problem+json` (RFC 9457) with a stable `code`.
Errors declared as `manifest.StatusError` -- wyrd's own sentinels are -- carry
their status and code; anything else uses the fallback status.

### Middleware: `RequireIfMatch`
Requires `If-Match` on `PUT`, `PATCH` and `DELETE` (428 when missing). The handler
reads the version with `IfMatchVersion(ctx)` and passes it to the store's version
guard. `Found` responses carry `ETag: "<version>"`.

### Middleware: `Idempotent`
Makes `POST` safe to retry with an `Idempotency-Key` header: the key is reserved
before the handler runs, the outcome is replayed to retries, and a key reused for
a different request is refused. See its doc comment for what it does not promise.

```go
    api.Use(bark.Idempotent(dbstore.NewIdempotencyStore(db, time.Minute), bark.IdempotencyOptions{
        Required: true,
        Scope:    func(ctx *gin.Context) string { return principalID(ctx) },
    }))
```

### Middleware: `AuthBearerAPI`

`AuthBearerAPI` extracts syntactically valid RFC 6750 Bearer credentials and
stores the token in the Gin context. The scheme is case-insensitive; one or more
ASCII spaces must separate it from a non-empty `b64token`. Tabs, leading or
trailing whitespace, credential lists, and malformed token characters are
rejected.

Malformed credentials stop the handler chain with one safe `401 Unauthorized`
response and `WWW-Authenticate: Bearer`. The response and parser errors never
contain the supplied header or token. This middleware validates syntax only; it
does not authenticate the token or make authorization decisions.

```go
api.GET("/artifacts",
    bark.AuthBearerAPI(),
    func(ctx *gin.Context) {
        token := bark.RequireBearerToken(ctx)
        // Authenticate token, then handle the request.
    },
)
```

Use `ParseBearer` when parsing outside Gin. It returns a
`*BearerParseError`; callers can inspect `Kind()` with `errors.As`. Every parse
failure also satisfies `errors.Is(err, bark.ErrInvalidAuthHeader)` for
compatibility.

### Middleware: `ResourceAPI`
Streamline implementation of APIs that serves a single resource.
### Middleware: `VersionedResourceAPI` 
Enables API implementation that can support request to versioned resources.
### Middleware: `ManifestAPI`
Helps to streamline implementation of APIs that detail with `manifest.Resource`
