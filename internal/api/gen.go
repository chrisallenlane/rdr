// Package api implements the rdr v1 JSON HTTP API.
//
// The OpenAPI spec at internal/api/openapi.yaml is the source of
// truth. The server interface and types in *.gen.go are produced by
// oapi-codegen and committed to the repo. CI verifies that
// `go generate ./...` produces no diff.
//
// Hand-written code lives in handlers.go (implementing the generated
// interface), server.go (constructor and mounting), errors.go (RFC
// 7807 helpers), spec.go (spec serving), and middleware.go (added in
// later tickets).
package api

//go:generate go tool oapi-codegen -config gen.yaml openapi.yaml
