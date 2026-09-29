package http

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	webtext "github.com/TryEarful/earful/web/text"
)

func bothLanguages(t *testing.T) (en, es uitext.Localizer) {
	t.Helper()
	catalog, err := uitext.Load(webtext.FS, uitext.Options{Strict: true, Languages: []string{"en", "es"}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return catalog.Localizer("en"), catalog.Localizer("es")
}

// everyError is one of each error a person can be shown.
func everyError() []error {
	errs := []error{
		domain.ErrAnswerTooLong, domain.ErrBadScale, domain.ErrDraftTooLong, domain.ErrTooManyLanguages,
		domain.LimitError{Kind: domain.LimitQuestionText, Limit: 500},
		domain.LimitError{Kind: domain.LimitTitle, Limit: 200},
		domain.QuestionError{Position: 2, Err: domain.ErrTooFewOptions},
		domain.QuestionError{Position: 7, Err: domain.LimitError{Kind: domain.LimitQuestionText, Limit: 500}},
		store.ErrImportTooLarge,
	}
	for _, plain := range plainErrors {
		errs = append(errs, plain.err)
	}
	return errs
}

// The English a person is shown is the English the error has always
// had. The message is where it is kept now, and the error's own text is
// what the log reads; the two are the same sentence, and this is what
// keeps them so.
func TestAnErrorIsWordedAsItDescribesItself(t *testing.T) {
	en, _ := bothLanguages(t)
	for _, err := range everyError() {
		got, ok := errorText(en, err)
		if !ok {
			t.Errorf("%v has no message", err)
			continue
		}
		if got != err.Error() {
			t.Errorf("worded %q, describes itself as %q", got, err.Error())
		}
	}
}

func TestAnErrorIsWordedForItsReader(t *testing.T) {
	_, es := bothLanguages(t)
	for err, want := range map[error]string{
		domain.ErrRequiredAnswer: "esta pregunta necesita una respuesta",
		domain.QuestionError{Position: 2, Err: domain.ErrTooFewOptions}: "pregunta 2: indique al menos dos opciones",
		domain.LimitError{Kind: domain.LimitTitle, Limit: 200}:          "que el título no pase de 200 caracteres",
	} {
		if got, _ := errorText(es, err); got != want {
			t.Errorf("%v worded %q, want %q", err, got, want)
		}
	}
	for _, err := range everyError() {
		got, ok := errorText(es, err)
		if !ok || got == "" || got == err.Error() {
			t.Errorf("%v is worded %q in Spanish", err, got)
		}
	}
}

// What is wrapped for the log is still the error it wraps.
func TestAWrappedErrorIsStillWorded(t *testing.T) {
	en, _ := bothLanguages(t)
	wrapped := fmt.Errorf("store: save draft: %w", domain.ErrDuplicateOption)
	if got, ok := errorText(en, wrapped); !ok || got != "two options are identical" {
		t.Errorf("worded %q, %v", got, ok)
	}
}

// An error with no message is the service's failure, and is not shown
// beside a form as though somebody could correct it.
func TestAFailureIsNotAValidationError(t *testing.T) {
	for _, err := range []error{
		errors.New("connection refused"),
		store.ErrNotFound,
		fmt.Errorf("question 3: %w", errors.New("connection refused")),
		nil,
	} {
		if isUserError(err) {
			t.Errorf("%v is taken for something a person did", err)
		}
	}
	for _, err := range everyError() {
		if !isUserError(err) {
			t.Errorf("%v is taken for a failure of the service", err)
		}
	}
}

// A new validation error needs a message, and this is what says so: the
// name of every error the domain declares is looked for in errtext.go.
func TestEveryValidationErrorIsNamedInErrtext(t *testing.T) {
	source, err := os.ReadFile("errtext.go")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("..", "domain", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if !strings.HasPrefix(name.Name, "Err") {
						continue
					}
					found++
					if !strings.Contains(string(source), name.Name) {
						t.Errorf("domain.%s has no message: give it one in errtext.go", name.Name)
					}
				}
			}
		}
	}
	if found < 15 {
		t.Errorf("found %d errors in the domain, which is fewer than there are", found)
	}
}
