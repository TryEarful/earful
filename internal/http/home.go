package http

import (
	"net/http"

	"github.com/a-h/templ"

	"github.com/TryEarful/earful/web/templates"
)

func homeHandler() http.Handler {
	return templ.Handler(templates.Home())
}
