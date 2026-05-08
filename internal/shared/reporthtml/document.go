package reporthtml

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"regexp"
	"strings"
)

const ContentType = "text/html; charset=utf-8"

var unsafeFilenameCharPattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Document is one runtime-rendered HTML report response.
type Document struct {
	HTML     []byte
	Filename string
}

// Renderer renders named report templates from application view models.
type Renderer interface {
	Render(name string, data any) ([]byte, error)
}

// TemplateRenderer executes HTML templates loaded from an fs.FS.
type TemplateRenderer struct {
	templates *template.Template
}

// NewTemplateRenderer parses report templates from the provided filesystem.
func NewTemplateRenderer(fsys fs.FS, patterns ...string) (*TemplateRenderer, error) {
	templates, err := template.ParseFS(fsys, patterns...)
	if err != nil {
		return nil, fmt.Errorf("parse report templates: %w", err)
	}
	return &TemplateRenderer{templates: templates}, nil
}

// Render executes one named template and returns its HTML bytes.
func (r *TemplateRenderer) Render(name string, data any) ([]byte, error) {
	if r == nil || r.templates == nil {
		return nil, fmt.Errorf("report template renderer is not configured")
	}

	var buf bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("render report template %q: %w", name, err)
	}
	return buf.Bytes(), nil
}

// HTMLFilename builds a conservative browser-safe HTML filename.
func HTMLFilename(prefix string, parts ...string) string {
	segments := make([]string, 0, len(parts))
	if cleaned := filenameSegment(prefix); cleaned != "" {
		segments = append(segments, cleaned)
	}
	for _, part := range parts {
		if cleaned := filenameSegment(part); cleaned != "" {
			segments = append(segments, cleaned)
		}
	}
	if len(segments) == 0 {
		return "report.html"
	}
	return strings.Join(segments, "-") + ".html"
}

// InlineContentDisposition returns a header value for browser preview.
func InlineContentDisposition(filename string) string {
	cleaned := filenameSegment(strings.TrimSuffix(filename, ".html"))
	if cleaned == "" {
		cleaned = "report"
	}
	return fmt.Sprintf(`inline; filename="%s.html"`, cleaned)
}

func filenameSegment(value string) string {
	cleaned := strings.Trim(unsafeFilenameCharPattern.ReplaceAllString(value, "-"), "-._")
	cleaned = strings.ToLower(cleaned)
	return cleaned
}
