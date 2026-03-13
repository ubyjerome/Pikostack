package main

import "embed"

//go:embed web/templates
var TemplateFS embed.FS

//go:embed web/static
var StaticFS embed.FS
