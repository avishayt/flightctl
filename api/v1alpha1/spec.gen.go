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
	"GnhpAQA=",
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
