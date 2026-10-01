package templates

import (
	"context"
	"unicode"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/TryEarful/earful/internal/uitext"
)

// t, tn and thtml word a template in the language of the request it is
// drawn for, which templ carries in ctx. They are short because a
// template names a message wherever it used to contain one.

// t renders a message.
func t(ctx context.Context, id uitext.ID, args ...uitext.Args) string {
	return uitext.From(ctx).T(id, args...)
}

// tn renders the form of a message that suits count.
func tn(ctx context.Context, id uitext.ID, count int, args ...uitext.Args) string {
	return uitext.From(ctx).N(id, count, args...)
}

// thtml renders a message that contains markup, as markup. The values
// in args are escaped; the message is written into the page as it is.
func thtml(ctx context.Context, id uitext.ID, args ...uitext.Args) templ.Component {
	return templ.Raw(uitext.From(ctx).HTML(id, args...))
}

// interfaceLang is the language the page is worded in.
func interfaceLang(ctx context.Context) string {
	return uitext.From(ctx).Lang()
}

// scriptText is what jsText puts on the page.
type scriptText struct {
	Lang     string         `json:"lang"`
	Messages map[string]any `json:"messages"`
}

// jsText gives the page's scripts their wording: the messages named, or
// whose names begin with a name given, in the language of the page. It
// is a block of JSON and not a script, which is all the
// Content-Security-Policy allows a page to carry inline (ADR-0006).
// uitext.js reads it.
func jsText(ctx context.Context, names ...string) templ.Component {
	l := uitext.From(ctx)
	return templ.JSONScript("interface-text", scriptText{Lang: l.Lang(), Messages: l.ForScripts(names...)})
}

type pathKey struct{}

// WithPath returns a context that knows the address of the page being
// drawn, for the language switcher to come back to.
func WithPath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, pathKey{}, path)
}

func currentPath(ctx context.Context) string {
	path, _ := ctx.Value(pathKey{}).(string)
	if path == "" {
		return "/"
	}
	return path
}

type themeKey struct{}

// WithTheme returns a context that knows the theme the reader chose:
// "light", "dark", or "" to follow their system.
func WithTheme(ctx context.Context, theme string) context.Context {
	return context.WithValue(ctx, themeKey{}, theme)
}

func theme(ctx context.Context) string {
	t, _ := ctx.Value(themeKey{}).(string)
	return t
}

// The ground of the page in each theme, for the browser's own chrome.
// They repeat --paper and --ink in web/static/css/app.css.
const (
	lightGround = "#F7F2E8"
	darkGround  = "#101823"
)

// themeColor is the colour the browser's chrome takes where the system
// asks for scheme: the ground of that scheme, or of the theme the reader
// chose over it.
func themeColor(ctx context.Context, scheme string) string {
	if t := theme(ctx); t != "" {
		scheme = t
	}
	if scheme == "dark" {
		return darkGround
	}
	return lightGround
}

// languages are the languages the page can be read in.
func languages(ctx context.Context) []string {
	return uitext.From(ctx).Languages()
}

// OwnName is the message for the name a language gives itself, which
// is how a language is offered to somebody who may not read the one the
// page is in.
func OwnName(lang string) uitext.ID {
	switch lang {
	case "en":
		return "switcher.own.en"
	case "es":
		return "switcher.own.es"
	}
	return ""
}

func ownName(ctx context.Context, lang string) string {
	return Named(uitext.From(ctx), OwnName(lang), lang)
}

// sentence starts a message fragment with a capital, for where it stands
// alone rather than after a colon. The fragments are written in lower
// case because the error summary puts them after one, which Spanish
// requires to stay lower case.
func sentence(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
