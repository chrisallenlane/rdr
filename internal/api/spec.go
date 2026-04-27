package api

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"

	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var specYAML []byte

// specJSON is the YAML spec rendered as JSON, computed once at startup.
var specJSON []byte

func init() {
	var doc any
	if err := yaml.Unmarshal(specYAML, &doc); err != nil {
		panic("api: invalid embedded openapi.yaml: " + err.Error())
	}
	doc = convertMapKeysToString(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		panic("api: cannot render openapi.yaml as JSON: " + err.Error())
	}
	specJSON = out
}

func serveSpecYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	if _, err := w.Write(specYAML); err != nil {
		slog.Warn("api: writing openapi.yaml", "error", err)
	}
}

func serveSpecJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write(specJSON); err != nil {
		slog.Warn("api: writing openapi.json", "error", err)
	}
}

// convertMapKeysToString walks a value produced by yaml.Unmarshal and
// converts every map[any]any (or map[interface{}]interface{}) to
// map[string]any so it can be encoded as JSON. yaml.v3's "any" decode
// path produces string keys natively for top-level maps but nested
// values may still need normalization.
func convertMapKeysToString(v any) any {
	switch t := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			ks, ok := k.(string)
			if !ok {
				continue
			}
			out[ks] = convertMapKeysToString(val)
		}
		return out
	case map[string]any:
		for k, val := range t {
			t[k] = convertMapKeysToString(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = convertMapKeysToString(val)
		}
		return t
	default:
		return v
	}
}
