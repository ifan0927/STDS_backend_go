package docs

import _ "embed"

var (
	//go:embed spec/openapi.yaml
	OpenAPI []byte
)
