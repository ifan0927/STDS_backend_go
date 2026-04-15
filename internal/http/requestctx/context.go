package requestctx

import "github.com/gin-gonic/gin"

const (
	requestIDKey = "request_id"
	principalKey = "principal"
	errorCodeKey = "error_code"
)

type Principal struct {
	UserID              string
	FirebaseUID         string
	Role                string
	AssignedPropertyIDs []string
}

func SetRequestID(c *gin.Context, requestID string) {
	c.Set(requestIDKey, requestID)
}

func GetRequestID(c *gin.Context) string {
	value, _ := c.Get(requestIDKey)
	text, _ := value.(string)

	return text
}

func SetPrincipal(c *gin.Context, principal Principal) {
	c.Set(principalKey, principal)
}

func GetPrincipal(c *gin.Context) (Principal, bool) {
	value, ok := c.Get(principalKey)
	if !ok {
		return Principal{}, false
	}

	principal, ok := value.(Principal)

	return principal, ok
}

func SetErrorCode(c *gin.Context, errorCode string) {
	c.Set(errorCodeKey, errorCode)
}

func GetErrorCode(c *gin.Context) string {
	value, _ := c.Get(errorCodeKey)
	text, _ := value.(string)

	return text
}
