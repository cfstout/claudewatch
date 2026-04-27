// claudewatch is a local daemon + CLI for tracking Claude Code session state
// across many tmux worktrees. The same binary runs both modes, dispatched by
// argv[1]. The "cw" symlink behaves identically.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cfstout/claudewatch/internal/cli"
	"github.com/cfstout/claudewatch/internal/config"
	"github.com/cfstout/claudewatch/internal/server"
	"github.com/cfstout/claudewatch/internal/state"
)

func main() {
	cfg := config.Load("")

	// "daemon" runs the server; everything else is a CLI subcommand. With no
	// args, the default subcommand is "pending".
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		os.Exit(runDaemon(cfg, os.Args[2:]))
	}

	baseURL := fmt.Sprintf("http://localhost:%d", cfg.Port)
	os.Exit(cli.Run(os.Args[1:], baseURL, cfg.DefaultSnoozeMinutes, os.Stdout, os.Stderr))
}

func runDaemon(cfg config.Config, args []string) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	port := fs.Int("port", cfg.Port, "TCP port to listen on (localhost only)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	store := state.NewStore()
	srv := server.New(store)
	srv.Notifier = server.NewOSNotifier(time.Duration(cfg.DebounceSeconds) * time.Second)
	srv.DefaultSnooze = time.Duration(cfg.DefaultSnoozeMinutes) * time.Minute

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("claudewatch listening", "addr", addr)

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil {
			slog.Error("server crashed", "err", err)
			return 1
		}
	case sig := <-sigCh:
		slog.Info("shutting down", "signal", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}
	return 0
}
