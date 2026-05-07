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

// MustNewBillReceiptRenderer returns the renderer for bill receipt templates.
func MustNewBillReceiptRenderer() reporthtml.Renderer {
	renderer, err := reporthtml.NewTemplateRenderer(reportTemplateFS, "templates/*.html")
	if err != nil {
		panic(err)
	}
	return renderer
}

// MustNewMonthlyCashflowRenderer returns the renderer for monthly cashflow templates.
func MustNewMonthlyCashflowRenderer() reporthtml.Renderer {
	renderer, err := reporthtml.NewTemplateRenderer(reportTemplateFS, "templates/*.html")
	if err != nil {
		panic(err)
	}
	return renderer
}

// MustNewProfitLossRenderer returns the renderer for profit and loss templates.
func MustNewProfitLossRenderer() reporthtml.Renderer {
	renderer, err := reporthtml.NewTemplateRenderer(reportTemplateFS, "templates/*.html")
	if err != nil {
		panic(err)
	}
	return renderer
}

// MustNewOperationReportRenderer returns the renderer for operation report templates.
func MustNewOperationReportRenderer() reporthtml.Renderer {
	renderer, err := reporthtml.NewTemplateRenderer(reportTemplateFS, "templates/*.html")
	if err != nil {
		panic(err)
	}
	return renderer
}
