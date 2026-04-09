package rdr

import "embed"

//go:embed all:static
var StaticFiles embed.FS

//go:embed all:templates
var TemplateFiles embed.FS
