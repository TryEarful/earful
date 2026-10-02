package templates

import (
	"context"
	"unicode"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/TryEarful/earful/internal/domain"
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

type modeKey struct{}

// WithMode returns a context that knows the display mode the reader
// chose: "light", "dark", or "" to follow their system.
func WithMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, modeKey{}, mode)
}

func mode(ctx context.Context) string {
	m, _ := ctx.Value(modeKey{}).(string)
	return m
}

// ground is the colour of the page in each display mode, for the
// browser's own chrome.
type ground struct{ light, dark string }

// themeGrounds repeats --bg from web/static/css/app.css for every theme,
// in both display modes; a test fails the build if the two disagree.
// The default theme's are --paper and --ink.
var themeGrounds = map[string]ground{
	domain.ThemeEarful: {light: "#F7F2E8", dark: "#101823"},
	domain.ThemeSlate:  {light: "#F1F3F5", dark: "#121416"},
	domain.ThemeOcean:  {light: "#EEF4FB", dark: "#081A36"},
	domain.ThemeForest: {light: "#EFF5ED", dark: "#0D1711"},
}

// groundOf is the ground of a theme, or of the default where the theme
// is not one the stylesheet has.
func groundOf(theme string) ground {
	if g, ok := themeGrounds[theme]; ok {
		return g
	}
	return themeGrounds[domain.ThemeEarful]
}

// modeColor is the colour the browser's chrome takes where the system
// asks for scheme: the theme's ground in that scheme, or in the display
// mode the reader chose over it.
func modeColor(ctx context.Context, scheme string, theme string) string {
	if m := mode(ctx); m != "" {
		scheme = m
	}
	if scheme == "dark" {
		return groundOf(theme).dark
	}
	return groundOf(theme).light
}

// themeClass is the class that draws a page in a survey's theme. The
// default theme has none: it is what the stylesheet draws without one.
// The name is one of the stylesheet's own, never the stored value
// written through, so nothing a draft holds can put a class on a page.
func themeClass(style domain.Style) string {
	switch style.Theme {
	case domain.ThemeSlate:
		return "theme-slate"
	case domain.ThemeOcean:
		return "theme-ocean"
	case domain.ThemeForest:
		return "theme-forest"
	}
	return ""
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
