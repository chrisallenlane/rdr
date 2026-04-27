package api

import (
	"encoding/json"
	"net/http"
)

// GetHealthz returns a static OK response. Public — no authentication.
func (s *Server) GetHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(HealthStatus{Status: Ok})
}
