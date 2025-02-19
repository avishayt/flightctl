package transport

import (
	"net/http"

	"github.com/flightctl/flightctl/internal/api/server"
	"github.com/flightctl/flightctl/pkg/version"
)

// (GET /api/version)
func (h *TransportHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	versionInfo := version.Get()
	return server.GetVersion200JSONResponse{
		Version: versionInfo.GitVersion,
	}, nil
}
