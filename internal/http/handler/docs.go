package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	projectdocs "stds_backend/docs"
)

// DocsHandler serves the generated OpenAPI document and interactive API docs.
type DocsHandler struct{}

// NewDocsHandler returns a handler for documentation endpoints.
func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

// OpenAPI serves the generated OpenAPI YAML document.
func (h *DocsHandler) OpenAPI(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", projectdocs.OpenAPI)
}

// Scalar serves the Scalar API reference UI configured for this service.
func (h *DocsHandler) Scalar(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>STDS API Docs</title>
  </head>
  <body>
    <div id="app"></div>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
    <script>
      Scalar.createApiReference('#app', {
        url: '/openapi.yaml',
        theme: 'saturn',
      })
    </script>
  </body>
</html>`))
}
