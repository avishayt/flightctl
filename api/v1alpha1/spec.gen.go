// Package v1alpha1 provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version v2.3.0 DO NOT EDIT.
package v1alpha1

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"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",
	"/yWYD7bUkwEA",
}

// GetSwagger returns the content of the embedded swagger specification file
// or error if failed to decode
func decodeSpec() ([]byte, error) {
	zipped, err := base64.StdEncoding.DecodeString(strings.Join(swaggerSpec, ""))
	if err != nil {
		return nil, fmt.Errorf("error base64 decoding spec: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %w", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(zr)
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %w", err)
	}

	return buf.Bytes(), nil
}

var rawSpec = decodeSpecCached()

// a naive cached of a decoded swagger spec
func decodeSpecCached() func() ([]byte, error) {
	data, err := decodeSpec()
	return func() ([]byte, error) {
		return data, err
	}
}

// Constructs a synthetic filesystem for resolving external references when loading openapi specifications.
func PathToRawSpec(pathToFile string) map[string]func() ([]byte, error) {
	res := make(map[string]func() ([]byte, error))
	if len(pathToFile) > 0 {
		res[pathToFile] = rawSpec
	}

	return res
}

// GetSwagger returns the Swagger specification corresponding to the generated code
// in this file. The external references of Swagger specification are resolved.
// The logic of resolving external references is tightly connected to "import-mapping" feature.
// Externally referenced files must be embedded in the corresponding golang packages.
// Urls can be supported but this task was out of the scope.
func GetSwagger() (swagger *openapi3.T, err error) {
	resolvePath := PathToRawSpec("")

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.ReadFromURIFunc = func(loader *openapi3.Loader, url *url.URL) ([]byte, error) {
		pathToFile := url.String()
		pathToFile = path.Clean(pathToFile)
		getSpec, ok := resolvePath[pathToFile]
		if !ok {
			err1 := fmt.Errorf("path not found: %s", pathToFile)
			return nil, err1
		}
		return getSpec()
	}
	var specData []byte
	specData, err = rawSpec()
	if err != nil {
		return
	}
	swagger, err = loader.LoadFromData(specData)
	if err != nil {
		return
	}
	return
}
