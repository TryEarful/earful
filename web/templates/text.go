package templates

import (
	"context"
	"strings"
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

// accountCurrent is how the header's Account item marks where the reader
// is: "page" on the account page itself, "true" on a page inside the
// account (its style, its translations), and "" elsewhere.
func accountCurrent(ctx context.Context) string {
	switch path := currentPath(ctx); {
	case path == "/account":
		return "page"
	case strings.HasPrefix(path, "/account/"):
		return "true"
	}
	return ""
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

type styleImagesKey struct{}

// WithStyleImages returns a context whose pages fetch a style's pictures
// from under prefix. A respondent's page takes them from the public
// address, which serves what a published version shows; a page drawn
// from a draft, the preview and the Style tab, takes them from the
// creator's own address for the survey, since nothing published shows
// them yet.
func WithStyleImages(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, styleImagesKey{}, prefix)
}

type suspensionKey struct{}

// WithSuspension returns a context whose pages tell a creator that an
// operator has suspended their workspace (ADR-0018), and whom to ask:
// contact is the instance's published address, or "" when it has none.
func WithSuspension(ctx context.Context, contact string) context.Context {
	return context.WithValue(ctx, suspensionKey{}, &contact)
}

// WithoutSuspensionNotice returns a context whose page does not carry
// the notice, for the page that is itself about the suspension.
func WithoutSuspensionNotice(ctx context.Context) context.Context {
	return context.WithValue(ctx, suspensionKey{}, (*string)(nil))
}

// suspension is whether the page is drawn for a suspended workspace, and
// the address to ask about it.
func suspension(ctx context.Context) (suspended bool, contact string) {
	c, _ := ctx.Value(suspensionKey{}).(*string)
	if c == nil {
		return false, ""
	}
	return true, *c
}

// Report is where a respondent's page sends somebody reporting the
// survey: a mailto: address naming the survey, or nothing when the
// instance publishes no address. The handler that finds the survey
// fills it in, after the page's context was made, since a survey is
// found in several places on the way to a page.
type Report struct {
	Href string
}

type reportKey struct{}

// WithReport returns a context carrying an empty Report to be filled in.
func WithReport(ctx context.Context) context.Context {
	return context.WithValue(ctx, reportKey{}, &Report{})
}

// ReportFrom is the Report the context carries, or nil.
func ReportFrom(ctx context.Context) *Report {
	report, _ := ctx.Value(reportKey{}).(*Report)
	return report
}

func reportHref(ctx context.Context) string {
	if report := ReportFrom(ctx); report != nil {
		return report.Href
	}
	return ""
}

// publicStyleImages is where anybody fetches a published style's
// pictures.
const publicStyleImages = "/style-image/"

// styleImageURL is the address of one of a style's pictures. It is the
// picture's hash under a prefix of Earful's own, so the page stays first
// party (ADR-0006) and the address can be kept by a browser for good:
// other bytes would have another address.
func styleImageURL(ctx context.Context, image domain.StyleImage) string {
	prefix, _ := ctx.Value(styleImagesKey{}).(string)
	if prefix == "" {
		prefix = publicStyleImages
	}
	return prefix + image.SHA256
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
