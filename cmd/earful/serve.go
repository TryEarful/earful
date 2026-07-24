package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/clock"
	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/email"
	apphttp "github.com/TryEarful/earful/internal/http"
	"github.com/TryEarful/earful/internal/logging"
)

func runServe(ctx context.Context, _ []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cfg.RequireDatabaseURL(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	logger := logging.New(cfg.LogLevel, os.Stdout)

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("serve: create db pool", "error", err)
		return 1
	}
	defer pool.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = pool.Ping(pingCtx)
	cancel()
	if err != nil {
		logger.Error("serve: database unreachable", "error", err)
		return 1
	}

	// Google OIDC is optional (self-hosters can run magic-link only). A
	// failed discovery downgrades to disabled rather than refusing to
	// serve — surveys must not go down because accounts.google.com
	// hiccuped at boot.
	var google *auth.GoogleOIDC
	if cfg.GoogleLoginEnabled() {
		oidcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		google, err = auth.NewGoogleOIDC(oidcCtx, cfg.GoogleIssuer,
			cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.BaseURL+"/auth/google/callback")
		cancel()
		if err != nil {
			logger.Warn("serve: google login disabled (OIDC discovery failed)", "error", err)
			google = nil
		}
	}

	var sender email.Sender
	switch cfg.EmailSender {
	case "smtp":
		sender = &email.SMTP{Addr: cfg.SMTPAddr, From: cfg.EmailFrom, User: cfg.SMTPUser, Pass: cfg.SMTPPass}
	case "brevo":
		sender = &email.Brevo{APIKey: cfg.BrevoAPIKey, From: cfg.EmailFrom}
	default:
		sender = email.NewConsole(os.Stdout)
	}

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Port),
		Handler: apphttp.NewHandler(cfg, logger, apphttp.Deps{
			Pool:   pool,
			Clock:  clock.Real{},
			Email:  sender,
			Google: google,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("serve: listening", "port", cfg.Port, "env", cfg.Env,
			"google_login", google != nil)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve: listen error", "error", err)
			return 1
		}
		return 0
	case <-ctx.Done():
		logger.Info("serve: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("serve: shutdown error", "error", err)
			return 1
		}
		return 0
	}
}
