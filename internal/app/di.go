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
)

type diContainer struct {
	storage *storage.Storage

	userRepo  *repo.UserRepo
	tokenRepo *repo.TokenRepo

	authService *service.AuthService

	tokenHandler *handler.TokenHandler
	userHandler  *handler.UserHandler
}

func newDIContainer() *diContainer {
	return &diContainer{}
}
func (d *diContainer) Storage(ctx context.Context, secrets *config.Secrets, cfg *config.Config) *storage.Storage {
	if d.storage == nil {
		stg, err := storage.NewStorage(ctx, secrets, cfg)
		if err != nil {
			slog.Error("не удалось подключиться к БД", "err", err)
			os.Exit(1)
		}

		closer.Add("база данных", func(_ context.Context) error {
			return stg.Close()
		})

		d.storage = stg
	}

	return d.storage
}

func (d *diContainer) UserRepo() repo.UserRepo {
	if d.userRepo == nil {
		d.userRepo = repo.NewUserRepo(d.Storage())
	}

	return d.userRepo
}

// AuthService возвращает сервис авторизации.
func (d *diContainer) AuthService() service.AuthService {
	if d.authService == nil {
		d.authService = service.NewAuthService(d.UserRepo(), d.SessionRepo(), d.Cache(), d.EventBus())
	}

	return d.authService
}

// Handler возвращает HTTP-хендлер.
func (d *diContainer) UserHandler() api.Handler {
	if d.handler == nil {
		d.handler = api.NewHandler(
			d.UserService(),
			d.AuthService(),
			d.NotificationService(),
		)
	}

	return d.handler
}
