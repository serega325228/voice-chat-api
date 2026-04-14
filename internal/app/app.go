package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"voice-chat-api/cmd/migrator"
	"voice-chat-api/internal/closer"
	"voice-chat-api/internal/config"
	"voice-chat-api/internal/lib/logger"
)

type App struct {
	container  *diContainer
	httpServer *http.Server
	cfg        *config.Config
}

func New() *App {
	cfg := config.MustLoad()
	secrets := config.MustLoadSecrets()
	migrator.MustMigrate(secrets.PostgresURL)

	log := logger.SetupLogger(cfg.Env)

	a := &App{
		container: newDIContainer(cfg, secrets, log),
		cfg:       cfg,
	}

	a.initDeps()

	return a
}

func (a *App) initDeps() {
	ctx := context.Background()

	a.container.Storage(ctx)
	a.container.Router(ctx)

	a.httpServer = &http.Server{
		Addr:         a.cfg.HTTPServer.Address,
		Handler:      a.container.Router(ctx),
		ReadTimeout:  a.cfg.HTTPServer.Timeout,
		WriteTimeout: a.cfg.HTTPServer.Timeout,
		IdleTimeout:  a.cfg.HTTPServer.IdleTimeout,
	}
}

func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	slog.Info("сервер запущен", "addr", a.cfg.HTTPServer.Address)

	go func() {
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("ошибка сервера", "err", err)
		}
	}()

	<-ctx.Done()
	slog.Info("получен сигнал, завершаем...")

	stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), a.cfg.HTTPServer.ShutdownTimeout)
	defer shutdownCancel()

	if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("ошибка при остановке сервера", "err", err)
	}

	slog.Info("сервер остановлен")

	closerCtx, closerCancel := context.WithTimeout(context.Background(), a.cfg.HTTPServer.CloseTimeout)
	defer closerCancel()

	if err := closer.CloseAll(closerCtx); err != nil {
		slog.Error("ошибки при закрытии ресурсов", "err", err)
	}

	return nil
}
