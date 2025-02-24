package transport

import (
	"encoding/json"
	"fmt"
	"net/http"

	api "github.com/flightctl/flightctl/api/v1alpha1"
)

const (
	Forbidden                      = "Forbidden"
	AuthorizationServerUnavailable = "Authorization server unavailable"
)

func SetResponse(w http.ResponseWriter, body any, status api.Status) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(int(status.Code))
	var err error
	if status.Code >= 200 && status.Code < 300 {
		err = json.NewEncoder(w).Encode(body)
	} else {
		err = json.NewEncoder(w).Encode(status)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func SetParseFailureResponse(w http.ResponseWriter, err error) {
	SetResponse(w, nil, api.StatusInternalServerError(fmt.Sprintf("can't decode JSON body: %v", err)))
}
