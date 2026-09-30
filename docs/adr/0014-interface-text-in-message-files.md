# Interface text lives in message files; a respondent's interface language is never stored

Every sentence, label and message the application shows is a named
message in `web/text/active.<lang>.toml`, one file per language, embedded
in the binary and served through `github.com/nicksnyder/go-i18n/v2`.
Templates, handlers and scripts name a message and never contain one.
English is the source; a message missing from another language is shown
in English.

The interface language is chosen per request and per audience:

| Audience | Read in order |
|---|---|
| Respondent (`/s/`, `/p/`) | the survey language in the address (`?lang=`), when the interface has it; the browser's `Accept-Language`; English |
| Everyone else | the `interface_lang` cookie set by the language switcher; `Accept-Language`; English |

Nothing about a respondent's language is stored, and the switcher's
cookie is not read on a respondent's page. This is story 25 and ADR-0003
carried over from the survey's language to the interface's: what
language a person reads is a fact about them.

Long-form writing is not a message. The trust page, the terms and the
help pages are documents: Markdown files in `web/pages`, one per
language, each served at the address its place in the folder gives it
(`help/voice.en.md` at `/help/voice`) and, as Markdown, at the same
address ending in `.md`. A document's front matter carries the hash of
its body and the day that hash last changed, written by `make pages`.

A document states facts about the instance serving it, and those differ
from one instance to the next. It names them (`{{.Region}}`,
`{{if .Brevo}}`) and the instance supplies them, so that every sentence
is in the file and every fact is the instance's own.

## Why

Two needs, one mechanism. The interface is to be offered in Spanish and
later in other languages; and its English wording is to be rewritten by
hand, sentence by sentence, by someone reading the sentences rather than
the templates. Both want the wording in one place, apart from the code
that shows it.

## Considered Options

- **The `goi18n` command's extract and merge.** `extract` reads Go
  source and cannot see a `.templ` file, so the English file is written
  by hand either way. `merge` rewrites every file from a map: comments
  and grouping are lost, and a translation whose English has changed is
  taken out of the served file until it is redone, so correcting a typo
  in English would show English to a Spanish reader. The library is used
  and the command is not. File names and the `hash` field follow its
  conventions, so it can still be used to begin a new language.
- **A language stored on the account.** It would follow a creator from
  one device to another, and needs a column, a setting, and a second
  rule for the pages before sign-in. A cookie covers those pages as well.
  Nothing here prevents adding the column later.
- **Translating with `golang.org/x/text/message`.** Its catalogs are
  built in Go and keyed by the English sentence, so rewording a sentence
  changes its key: the opposite of what the second need asks for.
- **One template per language.** No library at all, and every change to
  a page's structure made once per language.

- **The trust page as messages.** Thirty-five paragraphs as thirty-five
  entries in a TOML file can be translated and cannot be read. A
  document is written, reviewed and compared with its last version as a
  whole, which is what Markdown is for.
- **Rendering documents when they are built**, into HTML kept beside
  them. What a document says depends on the instance, which is not known
  until it starts; and a second copy of every document is a second thing
  to keep in step. They are rendered once, at startup.
- **The hash of the whole file.** A file cannot contain the hash of
  itself. The hash is of the body, which also means a new title does not
  move the date of the text.

## Consequences

- A message's name is an interface. It says where the message is and
  what it is for (`respond.submit.label`), never what it says, so that
  rewording changes no name.
- Names are written out where they are used and never assembled. A test
  reads the source for them and fails the build on a name with no
  message, a message with no use, or text left in a template.
- What is sent to a model, written to a log, or put in the header row of
  an export stays in English and stays in the code: those are read by
  programs, and one alert filter matches a log line to the letter.
- "Localization" and "translation" keep the meanings CONTEXT.md gives
  them. This is "interface text" in an "interface language".
- `<html lang>` on a respondent's page is the survey's language when one
  was chosen, since the questions are what the page is for, and the
  interface's otherwise. Dictation listens for the language of the
  questions, whatever the buttons are in: the survey's where one was
  chosen, and none where the survey is read as written, since nothing
  records what language a creator wrote in and a guess would be
  transcribed as though it were right. It takes this from an attribute
  of its own rather than from that declaration.
- Scripts receive their wording from the page, in a JSON block, because
  the Content-Security-Policy (ADR-0006) allows data there and code
  nowhere inline.
- HTML written in a document is not passed through, and a fact put into
  one is escaped first: what an operator configured is text, whatever
  characters it is spelled with.
- A document marked `draft` is loaded and checked like any other and
  given an address only in development. Terms that say "not written yet"
  are not terms anyone should be shown.
- A test fails the build on a document whose hash is not the hash of its
  body, so an edit cannot be released with the date of the text it
  replaced.
- The trust page is drawn from its headings rather than from a template,
  so what a paragraph looks like is decided by where it is. The
  paragraphs that were set in a quieter colour are now set like the rest.
