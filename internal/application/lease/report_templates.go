package lease

import (
	"embed"

	"stds_backend/internal/shared/reporthtml"
)

//go:embed templates/*.html
var leaseReportTemplateFS embed.FS

// MustNewCheckoutSettlementRenderer returns the renderer for checkout settlement templates.
func MustNewCheckoutSettlementRenderer() reporthtml.Renderer {
	renderer, err := reporthtml.NewTemplateRenderer(leaseReportTemplateFS, "templates/*.html")
	if err != nil {
		panic(err)
	}
	return renderer
}
