package templates

import (
	"context"

	"github.com/a-h/templ"

	"github.com/TryEarful/earful/internal/uitext"
)

// t and thtml word a template in the language of the request it is
// drawn for, which templ carries in ctx. They are short because a
// template names a message wherever it used to contain one.

// t renders a message.
func t(ctx context.Context, id uitext.ID, args ...uitext.Args) string {
	return uitext.From(ctx).T(id, args...)
}

// thtml renders a message that contains markup, as markup. The values
// in args are escaped; the message is written into the page as it is.
func thtml(ctx context.Context, id uitext.ID, args ...uitext.Args) templ.Component {
	return templ.Raw(uitext.From(ctx).HTML(id, args...))
}

// lang is the language the page is worded in, for its <html lang>.
func lang(ctx context.Context) string {
	return uitext.From(ctx).Lang()
}
