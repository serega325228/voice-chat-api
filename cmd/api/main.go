package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"voice-chat-api/cmd/migrator"
	"voice-chat-api/internal/config"
	"voice-chat-api/internal/handlers/token"
	"voice-chat-api/internal/handlers/user"
	"voice-chat-api/internal/lib/logger"
	"voice-chat-api/internal/services/auth"
	"voice-chat-api/internal/storage"
	storageToken "voice-chat-api/internal/storage/token"
	"voice-chat-api/internal/storage/transactor"
	storageUser "voice-chat-api/internal/storage/user"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg := config.MustLoad()
	secrets := config.MustLoadSecrets()
	migrator.MustMigrate(secrets.PostgresURL)

	log := logger.SetupLogger(cfg.Env)

	log.Info("starting voice-chat-api", slog.String("env", cfg.Env))
	log.Debug("debug messages are enabled")

	сtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := storage.New()
	if err != nil {
		log.Error("create storage", logger.Err(err))
		return
	}
	defer st.Close()

	tokensTx := transactor.New(st)

	userRepo := storageUser.New(tokensTx)
	tokenRepo := storageToken.New(tokensTx)

	authService := auth.New(
		log,
		userRepo,
		tokenRepo,
		tokensTx,
		cfg.AccessTTL,
		cfg.RefreshTTL,
		secrets.JWTSecret,
	)

	userHandler := user.New(log, authService)
	tokenHandler := token.New(log, authService)

	router := chi.NewMux()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(middleware.URLFormat)

	router.Route("/api", func(router chi.Router) {
		router.Route("/user", func(router chi.Router) {
			router.Post("/register", userHandler.Register)
			router.Post("/login", userHandler.Login)
		})
		router.Route("/token", func(router chi.Router) {
			router.Post("/refresh", tokenHandler.Refresh)
		})
	})

	log.Info("starting server", slog.String("address", cfg.Address))

	srv := &http.Server{
		Addr:         cfg.Address,
		Handler:      router,
		ReadTimeout:  cfg.HTTPServer.Timeout,
		WriteTimeout: cfg.HTTPServer.Timeout,
		IdleTimeout:  cfg.HTTPServer.IdleTimeout,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("failed to start server")
		}
	}()

	<-сtx.Done()
	log.Error("server stopping...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTPServer.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server stopping error")
	}

	//TODO CLOSER!!!

}
