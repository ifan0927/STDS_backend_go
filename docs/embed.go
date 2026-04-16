package docs

import _ "embed"

var (
	// OpenAPI contains the embedded OpenAPI specification served by the docs endpoint.
	//go:embed spec/openapi.yaml
	OpenAPI []byte
)
