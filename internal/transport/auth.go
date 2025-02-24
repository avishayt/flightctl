package transport

import (
	"encoding/json"
	"net/http"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/auth"
)

// (GET /api/v1/auth/config)
func (h *TransportHandler) AuthConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	authN := auth.GetAuthN()
	if _, ok := authN.(auth.NilAuth); ok {
		w.WriteHeader(http.StatusTeapot)
		return
	}
	w.WriteHeader(http.StatusOK)

	authConfig := authN.GetAuthConfig()

	conf := api.AuthConfig{
		AuthType: authConfig.Type,
		AuthURL:  authConfig.Url,
	}
	err := json.NewEncoder(w).Encode(conf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// (GET /api/v1/auth/validate)
func (h *TransportHandler) AuthValidate(w http.ResponseWriter, r *http.Request, params api.AuthValidateParams) {
	authn := auth.GetAuthN()
	if _, ok := authn.(auth.NilAuth); ok {
		w.WriteHeader(http.StatusTeapot)
		return
	}
	if params.Authorization == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	token, ok := auth.ParseAuthHeader(*params.Authorization)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	valid, err := authn.ValidateToken(r.Context(), token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(api.StatusInternalServerError(err.Error()))
		return
	}
	if !valid {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK)
}
