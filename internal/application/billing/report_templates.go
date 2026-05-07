package billing

import (
	"embed"

	"stds_backend/internal/shared/reporthtml"
)

//go:embed templates/*.html
var reportTemplateFS embed.FS

// MustNewTenantRosterRenderer returns the renderer for billing report templates.
func MustNewTenantRosterRenderer() reporthtml.Renderer {
	renderer, err := reporthtml.NewTemplateRenderer(reportTemplateFS, "templates/*.html")
	if err != nil {
		panic(err)
	}
	return renderer
}
