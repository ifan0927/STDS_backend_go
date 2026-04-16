package requestctx

import "github.com/gin-gonic/gin"

const (
	requestIDKey = "request_id"
	principalKey = "principal"
	errorCodeKey = "error_code"
)

// Principal carries the authenticated user identity attached to a request.
type Principal struct {
	UserID              string
	FirebaseUID         string
	Role                string
	AssignedPropertyIDs []string
}

// SetRequestID stores the request identifier in the Gin context for downstream
// middleware and handlers.
func SetRequestID(c *gin.Context, requestID string) {
	c.Set(requestIDKey, requestID)
}

// GetRequestID returns the request identifier stored in the Gin context.
func GetRequestID(c *gin.Context) string {
	value, _ := c.Get(requestIDKey)
	text, _ := value.(string)

	return text
}

// SetPrincipal stores the authenticated principal in the Gin context.
func SetPrincipal(c *gin.Context, principal Principal) {
	c.Set(principalKey, principal)
}

// GetPrincipal returns the authenticated principal from the Gin context when
// one has been set.
func GetPrincipal(c *gin.Context) (Principal, bool) {
	value, ok := c.Get(principalKey)
	if !ok {
		return Principal{}, false
	}

	principal, ok := value.(Principal)

	return principal, ok
}

// SetErrorCode stores the application error code for the current request.
func SetErrorCode(c *gin.Context, errorCode string) {
	c.Set(errorCodeKey, errorCode)
}

// GetErrorCode returns the application error code stored for the current
// request.
func GetErrorCode(c *gin.Context) string {
	value, _ := c.Get(errorCodeKey)
	text, _ := value.(string)

	return text
}
