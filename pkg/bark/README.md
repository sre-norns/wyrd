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

# Filters
The library offers a collection of middleware / filter that streamline implementation of a service responsible for a collection of resource. 

- `ContentTypeAPI` - enables API to support different serialization formats, respecting [`Accept`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept) HTTP Request headers.
- `SearchableAPI` - enables APIs to support query filter and pagination.
- `AuthBearerAPI` - enables APIs to read Auth Bearer token.
- `ResourceAPI` - streamline implementation of APIs that serves a single resource.
- `VersionedResourceAPI` - enables API implementation that can support request to versioned resources.
- `ManifestAPI` - helps to streamline implementation of APIs that detail with `manifest.Resource`



### Filters: `ContentTypeApi`
This function produces `gin.HandlerFunc` as middleware to add support for response marshaler selection based on [`Accept`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept) HTTP Request header.

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

