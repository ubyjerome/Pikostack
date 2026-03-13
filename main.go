package main

import (
	"github.com/pikostack/pikostack/cmd"
	"github.com/pikostack/pikostack/internal/api/handlers"
)

func main() {
	handlers.RegisterFS(TemplateFS, StaticFS)
	cmd.Execute()
}
