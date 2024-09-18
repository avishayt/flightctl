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
	"r/9/AAAA//8CkjqwoX0BAA==",
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
