package app

import (
	"context"
	"log/slog"
	"os"
	"voice-chat-api/internal/closer"
	"voice-chat-api/internal/config"
	handler "voice-chat-api/internal/handlers"
	repo "voice-chat-api/internal/repositories"
	service "voice-chat-api/internal/services"
	"voice-chat-api/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type diContainer struct {
	cfg          *config.Config
	secrets      *config.Secrets
	log          *slog.Logger
	storage      *storage.Storage
	transactor   *storage.Transactor
	userRepo     *repo.UserRepo
	tokenRepo    *repo.TokenRepo
	authService  *service.AuthService
	userHandler  *handler.UserHandler
	tokenHandler *handler.TokenHandler
	router       *chi.Mux
}

func newDIContainer(cfg *config.Config, secrets *config.Secrets, log *slog.Logger) *diContainer {
	return &diContainer{
		cfg:     cfg,
		secrets: secrets,
		log:     log,
	}
}

func (c *diContainer) Storage(ctx context.Context) *storage.Storage {
	if c.storage == nil {
		stg, err := storage.NewStorage(ctx, c.secrets, c.cfg)
		if err != nil {
			c.log.Error("failed to initialize storage", "err", err)
			os.Exit(1)
		}

		closer.Add("storage", func(_ context.Context) error {
			return stg.Close()
		})

		c.storage = stg
	}

	return c.storage
}

func (c *diContainer) Transactor(ctx context.Context) *storage.Transactor {
	if c.transactor == nil {
		c.transactor = storage.NewTransactor(c.Storage(ctx))
	}

	return c.transactor
}

func (c *diContainer) UserRepo(ctx context.Context) *repo.UserRepo {
	if c.userRepo == nil {
		c.userRepo = repo.NewUserRepo(c.Transactor(ctx))
	}

	return c.userRepo
}

func (c *diContainer) TokenRepo(ctx context.Context) *repo.TokenRepo {
	if c.tokenRepo == nil {
		c.tokenRepo = repo.NewTokenRepo(c.Transactor(ctx))
	}

	return c.tokenRepo
}

func (c *diContainer) AuthService(ctx context.Context) *service.AuthService {
	if c.authService == nil {
		c.authService = service.NewAuthService(
			c.log,
			c.UserRepo(ctx),
			c.TokenRepo(ctx),
			c.Transactor(ctx),
			c.cfg.JWT.AccessTTL,
			c.cfg.JWT.RefreshTTL,
			c.secrets.JWTSecret,
		)
	}

	return c.authService
}

func (c *diContainer) UserHandler(ctx context.Context) *handler.UserHandler {
	if c.userHandler == nil {
		c.userHandler = handler.NewUserHandler(c.log, c.AuthService(ctx))
	}

	return c.userHandler
}

func (c *diContainer) TokenHandler(ctx context.Context) *handler.TokenHandler {
	if c.tokenHandler == nil {
		c.tokenHandler = handler.NewTokenHandler(c.log, c.AuthService(ctx))
	}

	return c.tokenHandler
}

func (c *diContainer) Router(ctx context.Context) *chi.Mux {
	if c.router == nil {
		c.router = chi.NewMux()

		c.router.Use(middleware.RequestID)
		c.router.Use(middleware.RealIP)
		c.router.Use(middleware.Recoverer)
		c.router.Use(middleware.URLFormat)

		c.router.Route("/api", func(r chi.Router) {
			r.Route("/user", func(r chi.Router) {
				r.Post("/register", c.UserHandler(ctx).Register)
				r.Post("/login", c.UserHandler(ctx).Login)
			})
			r.Route("/token", func(r chi.Router) {
				r.Post("/refresh", c.TokenHandler(ctx).Refresh)
			})
		})
	}

	return c.router
}

func (c *diContainer) Config() *config.Config {
	return c.cfg
}

func (c *diContainer) Secrets() *config.Secrets {
	return c.secrets
}

func (c *diContainer) Logger() *slog.Logger {
	return c.log
}
