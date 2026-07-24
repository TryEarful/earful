package http

import (
	"context"

	"github.com/TryEarful/earful/internal/auth"
)

type ctxKey int

const authInfoKey ctxKey = iota

func withAuth(ctx context.Context, info auth.AuthInfo) context.Context {
	return context.WithValue(ctx, authInfoKey, info)
}

// authFrom returns the authenticated identity, if any. Handlers behind
// requireAuth can rely on ok == true.
func authFrom(ctx context.Context) (auth.AuthInfo, bool) {
	info, ok := ctx.Value(authInfoKey).(auth.AuthInfo)
	return info, ok
}
