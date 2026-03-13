package handlers

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

var (
	templateFS embed.FS
	staticFS   embed.FS
	fsInited   bool
)

// RegisterFS is called from main() with the embedded file systems
func RegisterFS(tmpl, static embed.FS) {
	templateFS = tmpl
	staticFS = static
	fsInited = true
}

// MustLoadTemplates loads templates or panics — called during router init
func MustLoadTemplates() {
	if !fsInited {
		log.Fatal("handlers: RegisterFS not called before router init")
	}
}

// GetTemplateFS returns the template FS for use in router setup
func GetTemplateFS() embed.FS { return templateFS }

// GetStaticFS returns the static FS for use in router setup
func GetStaticFS() embed.FS { return staticFS }

// StaticHandler serves embedded static files under /static/
func StaticHandler() gin.HandlerFunc {
	sub, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		panic("static fs: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	return func(c *gin.Context) {
		c.Request.URL.Path = c.Param("filepath")
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}
