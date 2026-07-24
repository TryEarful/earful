// Command earful is the single binary for the Earful survey platform:
// serve (HTTP server), migrate (goose migrations), purge (retention
// cleanup), healthcheck (container probe), beta-codes (private-beta
// invite codes, M12), and admin (super-admin grants, M12).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: earful <serve|migrate|purge|healthcheck|beta-codes|admin> [flags]")
		os.Exit(2)
	}

	var code int
	switch os.Args[1] {
	case "serve":
		code = runServe(ctx, os.Args[2:])
	case "migrate":
		code = runMigrate(ctx, os.Args[2:])
	case "purge":
		code = runPurge(ctx, os.Args[2:])
	case "healthcheck":
		code = runHealthcheck(ctx, os.Args[2:])
	case "beta-codes":
		code = runBetaCodes(ctx, os.Args[2:])
	case "admin":
		code = runAdmin(ctx, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		code = 2
	}
	os.Exit(code)
}
