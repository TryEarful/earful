package http

import (
	"net/http"

	"github.com/TryEarful/earful/internal/uitext"
)

// say renders a message in the language the request is answered in.
// Handlers word what they show through it, as templates do through t.
func say(r *http.Request, id uitext.ID, args ...uitext.Args) string {
	return uitext.From(r.Context()).T(id, args...)
}

// interfaceLanguage is the language a request is answered in.
func interfaceLanguage(r *http.Request) string {
	return uitext.From(r.Context()).Lang()
}

// interfaceText gives every request the wording it is answered in.
// Templates and handlers read it from the context, so a page is worded
// in one language throughout without any of them being told which.
func (s *server) interfaceText(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := uitext.With(r.Context(), s.text.Localizer())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
