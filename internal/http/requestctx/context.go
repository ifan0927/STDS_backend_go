package requestctx

import "github.com/gin-gonic/gin"

const (
	requestIDKey  = "request_id"
	principalKey  = "principal"
	errorCodeKey  = "error_code"
	propertyIDKey = "property_id"
	jobMetaKey    = "job_meta"
)

// Principal carries the authenticated user identity attached to a request.
type Principal struct {
	UserID              string
	FirebaseUID         string
	Role                string
	AssignedPropertyIDs []string
}

// JobMeta carries scheduler execution details for logging.
type JobMeta struct {
	JobKey     string
	WindowKey  string
	Status     string
	RetryCount int
	DurationMs int64
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

// SetPropertyID stores the authorized property ID for the current request.
func SetPropertyID(c *gin.Context, propertyID string) {
	c.Set(propertyIDKey, propertyID)
}

// GetPropertyID returns the authorized property ID stored for the current
// request.
func GetPropertyID(c *gin.Context) string {
	value, _ := c.Get(propertyIDKey)
	text, _ := value.(string)

	return text
}

// SetJobMeta stores scheduler execution details in the Gin context.
func SetJobMeta(c *gin.Context, meta JobMeta) {
	c.Set(jobMetaKey, meta)
}

// GetJobMeta returns scheduler execution details when present.
func GetJobMeta(c *gin.Context) (JobMeta, bool) {
	value, ok := c.Get(jobMetaKey)
	if !ok {
		return JobMeta{}, false
	}

	meta, ok := value.(JobMeta)
	return meta, ok
}
