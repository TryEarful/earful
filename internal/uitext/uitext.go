// Package uitext serves the interface's wording in the reader's
// language. The wording itself lives in web/text, one TOML file per
// language, so that a sentence can be rewritten or translated without
// touching a template; this package loads those files, picks a language
// for a request, and fills a message in.
//
// "Interface text" and "interface language" are the terms for this,
// because CONTEXT.md gives "localization" and "translation" to what is
// done to a survey's own questions and answers.
package uitext

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	texttemplate "text/template"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nicksnyder/go-i18n/v2/i18n/template"
	"golang.org/x/text/language"

	"github.com/TryEarful/earful/web/text"
)

// ID names a message: where it is and what it is for, as in
// "respond.submit.label". IDs are written out where they are used and
// never assembled, so that every use can be found by reading the source.
type ID string

// Args are the values for a message's placeholders, by name.
type Args map[string]any

// Source is the language the interface is written in. A message missing
// from another language is shown in this one.
const Source = "en"

// Served lists the languages the interface is offered in, the source
// first. A language whose file exists but which is not listed here is
// checked when the files are loaded and offered to nobody, so that a
// translation can be written over several changes without a reader ever
// meeting half of one.
var Served = []string{Source}

// HTMLSuffix ends the name of a message that contains markup.
const HTMLSuffix = "_html"

// Options adjust how a Catalog behaves.
type Options struct {
	// Strict makes a missing message, or a placeholder given no value,
	// a panic. Tests run strict, so that either fails the test that
	// reached it; a running service shows what it can and logs the rest.
	Strict bool
	// Languages overrides Served.
	Languages []string
	// Logger receives what a non-strict Catalog could not render.
	Logger *slog.Logger
}

// Catalog is every message in every language, ready to serve.
type Catalog struct {
	bundle  *i18n.Bundle
	served  []language.Tag
	matcher language.Matcher
	written map[string]map[ID]*i18n.Message
	strict  bool
	logger  *slog.Logger
	parser  template.Parser
}

var idPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

// Load reads every active.*.toml in fsys. It fails on anything that
// would otherwise surface as a broken page: a file that does not parse,
// a message with no text, a placeholder that is not closed. go-i18n
// parses a message the first time it is shown, which for a rarely seen
// error page could be months after the mistake was made.
func Load(fsys fs.FS, opts Options) (*Catalog, error) {
	names, err := fs.Glob(fsys, "active.*.toml")
	if err != nil {
		return nil, fmt.Errorf("uitext: list message files: %w", err)
	}
	sort.Strings(names)

	languages := opts.Languages
	if len(languages) == 0 {
		languages = Served
	}
	c := &Catalog{
		bundle:  i18n.NewBundle(language.MustParse(Source)),
		written: map[string]map[ID]*i18n.Message{},
		strict:  opts.Strict,
		logger:  opts.Logger,
		parser:  &template.TextParser{},
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	if c.strict {
		c.parser = &template.TextParser{Option: "missingkey=error"}
	}

	unmarshal := map[string]i18n.UnmarshalFunc{"toml": toml.Unmarshal}
	for _, name := range names {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("uitext: read %s: %w", name, err)
		}
		if err := checkNames(raw); err != nil {
			return nil, fmt.Errorf("uitext: %s: %w", name, err)
		}
		file, err := i18n.ParseMessageFileBytes(raw, name, unmarshal)
		if err != nil {
			return nil, fmt.Errorf("uitext: parse %s: %w", name, err)
		}
		lang := file.Tag.String()
		messages := make(map[ID]*i18n.Message, len(file.Messages))
		for _, msg := range file.Messages {
			if err := check(msg); err != nil {
				return nil, fmt.Errorf("uitext: %s: %w", name, err)
			}
			messages[ID(msg.ID)] = msg
		}
		c.written[lang] = messages
	}
	if _, ok := c.written[Source]; !ok {
		return nil, fmt.Errorf("uitext: no active.%s.toml: the interface has no text", Source)
	}

	for _, lang := range languages {
		messages, ok := c.written[lang]
		if !ok {
			return nil, fmt.Errorf("uitext: no active.%s.toml for a language that is served", lang)
		}
		tag := language.MustParse(lang)
		list := make([]*i18n.Message, 0, len(messages))
		for _, msg := range messages {
			list = append(list, msg)
		}
		if err := c.bundle.AddMessages(tag, list...); err != nil {
			return nil, fmt.Errorf("uitext: add %s: %w", lang, err)
		}
		c.served = append(c.served, tag)
	}
	c.matcher = language.NewMatcher(c.served)
	return c, nil
}

// checkNames refuses the one word go-i18n takes for its own wherever it
// appears. It reads a table named "translation" as the text of the
// table above, so the message that was meant disappears and another
// turns up under a shorter name; saying so here names the real mistake.
func checkNames(raw []byte) error {
	var tree map[string]any
	if err := toml.Unmarshal(raw, &tree); err != nil {
		return err
	}
	var walk func(path string, node map[string]any) error
	walk = func(path string, node map[string]any) error {
		for key, value := range node {
			name := key
			if path != "" {
				name = path + "." + key
			}
			if strings.EqualFold(key, "translation") {
				return fmt.Errorf("%q: no part of a name can be \"translation\"", name)
			}
			if child, ok := value.(map[string]any); ok {
				if err := walk(name, child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk("", tree)
}

// check refuses a message that could not be shown.
func check(msg *i18n.Message) error {
	if !idPattern.MatchString(msg.ID) {
		return fmt.Errorf("%q is not a message name: lowercase words joined by dots", msg.ID)
	}
	forms := Forms(msg)
	if len(forms) == 0 {
		// A name ending in a reserved word is read as a field of the
		// message above it, which leaves that message without text.
		return fmt.Errorf("%q has no text; a name cannot end in a field of a message such as \"description\" or \"other\"", msg.ID)
	}
	if forms["other"] == "" {
		return fmt.Errorf("%q has no \"other\" form, which every message needs", msg.ID)
	}
	left, right := msg.LeftDelim, msg.RightDelim
	if left == "" {
		left = "{{"
	}
	if right == "" {
		right = "}}"
	}
	for form, src := range forms {
		if _, err := texttemplate.New("").Delims(left, right).Parse(src); err != nil {
			return fmt.Errorf("%q (%s): %w", msg.ID, form, err)
		}
	}
	return nil
}

// Forms returns a message's wordings by plural form, leaving out the
// forms it does not have.
func Forms(msg *i18n.Message) map[string]string {
	forms := map[string]string{}
	for name, src := range map[string]string{
		"zero": msg.Zero, "one": msg.One, "two": msg.Two,
		"few": msg.Few, "many": msg.Many, "other": msg.Other,
	} {
		if src != "" {
			forms[name] = src
		}
	}
	return forms
}

// Written returns the messages of one language as they are in its file,
// whether or not the language is served. It is for the checks that
// compare one language with another.
func (c *Catalog) Written(lang string) map[ID]*i18n.Message { return c.written[lang] }

// Languages returns the languages served, the source first.
func (c *Catalog) Languages() []string {
	out := make([]string, len(c.served))
	for i, tag := range c.served {
		out[i] = tag.String()
	}
	return out
}

// Serves reports whether lang is a language the interface is offered
// in, written exactly as Languages writes it.
func (c *Catalog) Serves(lang string) bool {
	for _, tag := range c.served {
		if tag.String() == lang {
			return true
		}
	}
	return false
}

// Localizer picks the served language that best suits prefs, which are
// read in order: each is a language tag or a whole Accept-Language
// header, and an empty or unreadable one is passed over. With nothing
// to go on the language is the source.
func (c *Catalog) Localizer(prefs ...string) Localizer {
	var wanted []language.Tag
	for _, pref := range prefs {
		tags, _, err := language.ParseAcceptLanguage(pref)
		if err != nil {
			continue
		}
		wanted = append(wanted, tags...)
	}
	_, index, _ := c.matcher.Match(wanted...)
	lang := c.served[index].String()
	return Localizer{catalog: c, lang: lang, inner: i18n.NewLocalizer(c.bundle, lang)}
}

// Localizer renders messages in one language. It is a value, and holds
// for as long as it is kept: a socket or an email being sent has no
// request to ask, and carries the Localizer of the request that began it.
type Localizer struct {
	catalog *Catalog
	lang    string
	inner   *i18n.Localizer
}

// Lang is the language rendered, as a tag: "en", "es".
func (l Localizer) Lang() string { return l.lang }

// Languages are the languages a reader can choose between, the source
// first.
func (l Localizer) Languages() []string { return l.catalog.Languages() }

// T renders a message.
func (l Localizer) T(id ID, args ...Args) string {
	if strings.HasSuffix(string(id), HTMLSuffix) {
		// Rendered as text, markup is shown rather than obeyed, which is
		// wrong but harmless.
		l.catalog.fail(fmt.Errorf("uitext: %s contains markup and is rendered with HTML, not T", id))
	}
	return l.render(id, nil, merge(args))
}

// N renders the form of a message that suits count, which the message
// can show as {{.Count}}.
func (l Localizer) N(id ID, count int, args ...Args) string {
	data := merge(args)
	if data == nil {
		data = Args{}
	}
	data["Count"] = count
	return l.render(id, &count, data)
}

// HTML renders a message that contains markup. The message is trusted,
// being part of the program; the values put into it are not, and are
// escaped first. The result is safe to write into a page as it is.
func (l Localizer) HTML(id ID, args ...Args) string {
	data := merge(args)
	for name, value := range data {
		switch v := value.(type) {
		case int, int32, int64, uint, uint32, uint64, float32, float64, bool:
			// Cannot carry markup, and a message may need the number.
		case string:
			data[name] = html.EscapeString(v)
		default:
			data[name] = html.EscapeString(fmt.Sprint(v))
		}
	}
	out := l.render(id, nil, data)
	if !strings.HasSuffix(string(id), HTMLSuffix) {
		l.catalog.fail(fmt.Errorf("uitext: %s is rendered with HTML, and its name does not end in %s", id, HTMLSuffix))
		return html.EscapeString(out)
	}
	return out
}

func (l Localizer) render(id ID, count *int, data Args) string {
	config := &i18n.LocalizeConfig{
		MessageID:      string(id),
		TemplateParser: l.catalog.parser,
	}
	if data != nil {
		config.TemplateData = map[string]any(data)
	}
	if count != nil {
		config.PluralCount = *count
	}
	out, err := l.inner.Localize(config)
	if err != nil {
		var missing *i18n.MessageNotFoundErr
		if out != "" && errors.As(err, &missing) && !l.catalog.strict {
			// Not yet translated: the source wording stands in.
			return out
		}
		l.catalog.fail(fmt.Errorf("uitext: %s in %s: %w", id, l.lang, err))
	}
	if out == "" {
		// The name is better than a gap: it can be read, and searched for.
		return string(id)
	}
	return out
}

func (c *Catalog) fail(err error) {
	if c.strict {
		panic(err)
	}
	c.logger.Error(err.Error())
}

func merge(args []Args) Args {
	switch len(args) {
	case 0:
		return nil
	case 1:
		// Copied: N and HTML write to it, and the caller may keep theirs.
		out := make(Args, len(args[0])+1)
		for name, value := range args[0] {
			out[name] = value
		}
		return out
	}
	out := Args{}
	for _, set := range args {
		for name, value := range set {
			out[name] = value
		}
	}
	return out
}

// Embedded returns the Catalog of the files built into the binary. They
// are read once. A fault in them is a fault in the build, and panics.
var Embedded = sync.OnceValue(func() *Catalog {
	c, err := Load(text.FS, Options{})
	if err != nil {
		panic(err)
	}
	return c
})

type contextKey struct{}

// With returns a context that renders in l.
func With(ctx context.Context, l Localizer) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// From returns the Localizer a context renders in. A context that was
// given none renders the embedded text in the source language, so a
// component drawn outside a request still has its words.
func From(ctx context.Context) Localizer {
	if l, ok := ctx.Value(contextKey{}).(Localizer); ok {
		return l
	}
	return Embedded().Localizer()
}
