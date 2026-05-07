package reporthtml

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestTemplateRendererRendersNamedTemplate(t *testing.T) {
	renderer, err := NewTemplateRenderer(fstest.MapFS{
		"templates/report.html": {Data: []byte(`{{define "sample"}}<p>{{.Name}}</p>{{end}}`)},
	}, "templates/*.html")
	if err != nil {
		t.Fatalf("NewTemplateRenderer: %v", err)
	}

	html, err := renderer.Render("sample", struct{ Name string }{Name: "Tenant"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(html) != "<p>Tenant</p>" {
		t.Fatalf("html = %q", html)
	}
}

func TestTemplateRendererReturnsExecutionError(t *testing.T) {
	renderer, err := NewTemplateRenderer(fstest.MapFS{
		"templates/report.html": {Data: []byte(`{{define "sample"}}{{template "missing" .}}{{end}}`)},
	}, "templates/*.html")
	if err != nil {
		t.Fatalf("NewTemplateRenderer: %v", err)
	}

	_, err = renderer.Render("sample", map[string]string{})
	if err == nil {
		t.Fatal("expected render error")
	}
	if !strings.Contains(err.Error(), `render report template "sample"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTMLFilenameSanitizesSegments(t *testing.T) {
	got := HTMLFilename("Tenant Roster", "My Property", "2026/05/07")
	if got != "tenant-roster-my-property-2026-05-07.html" {
		t.Fatalf("filename = %q", got)
	}
}

func TestInlineContentDispositionUsesHTMLFilename(t *testing.T) {
	got := InlineContentDisposition(`Tenant Roster.html`)
	if got != `inline; filename="tenant-roster.html"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
}
