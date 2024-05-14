package bark

import "github.com/gin-gonic/gin"

// EventStreamAPI writes response headed required for Server Side Event response
func EventStreamAPI() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set(HTTPHeaderContentType, "text/event-stream")
		c.Writer.Header().Set(HTTPHeaderCacheControl, "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Header().Set("Transfer-Encoding", "chunked")
		c.Next()
	}
}
