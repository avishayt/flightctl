// Package v1alpha1 provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/deepmap/oapi-codegen version v1.15.0 DO NOT EDIT.
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
	"AAA=",
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
