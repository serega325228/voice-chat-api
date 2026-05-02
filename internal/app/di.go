package app

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"voice-chat-api/internal/closer"
	"voice-chat-api/internal/config"
	grpcsignaling "voice-chat-api/internal/grpc/signaling"
	handler "voice-chat-api/internal/handlers"
	mw "voice-chat-api/internal/middlewares"
	repo "voice-chat-api/internal/repositories"
	service "voice-chat-api/internal/services"
	"voice-chat-api/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type diContainer struct {
	cfg     *config.Config
	secrets *config.Secrets
	log     *slog.Logger

	storageOnce    sync.Once
	storage        *storage.Storage
	transactorOnce sync.Once
	transactor     *storage.Transactor
	userRepoOnce   sync.Once
	userRepo       *repo.UserRepo
	tokenRepoOnce  sync.Once
	tokenRepo      *repo.TokenRepo

	authServiceOnce sync.Once
	authService     *service.AuthService
	grpcConnOnce    sync.Once
	grpcConn        *grpc.ClientConn
	signalingOnce   sync.Once
	signalingSvc    *grpcsignaling.Service

	validatorOnce sync.Once
	validator     *validator.Validate

	userHandlerOnce  sync.Once
	userHandler      *handler.UserHandler
	tokenHandlerOnce sync.Once
	tokenHandler     *handler.TokenHandler
	wsHandlerOnce    sync.Once
	wsHandler        *handler.WSHandler
	routerOnce       sync.Once
	router           *chi.Mux
}

func newDIContainer(cfg *config.Config, secrets *config.Secrets, log *slog.Logger) *diContainer {
	return &diContainer{
		cfg:     cfg,
		secrets: secrets,
		log:     log,
	}
}

func (c *diContainer) Storage(ctx context.Context) *storage.Storage {
	c.storageOnce.Do(func() {
		stg, err := storage.NewStorage(ctx, c.secrets, c.cfg)
		if err != nil {
			c.log.Error("failed to initialize storage", "err", err)
			os.Exit(1)
		}

		closer.Add("storage", func(_ context.Context) error {
			return stg.Close()
		})

		c.storage = stg
	})

	return c.storage
}

func (c *diContainer) Transactor(ctx context.Context) *storage.Transactor {
	c.transactorOnce.Do(func() {
		c.transactor = storage.NewTransactor(c.Storage(ctx))
	})

	return c.transactor
}

func (c *diContainer) UserRepo(ctx context.Context) *repo.UserRepo {
	c.userRepoOnce.Do(func() {
		c.userRepo = repo.NewUserRepo(c.Transactor(ctx))
	})

	return c.userRepo
}

func (c *diContainer) TokenRepo(ctx context.Context) *repo.TokenRepo {
	c.tokenRepoOnce.Do(func() {
		c.tokenRepo = repo.NewTokenRepo(c.Transactor(ctx))
	})

	return c.tokenRepo
}

func (c *diContainer) AuthService(ctx context.Context) *service.AuthService {
	c.authServiceOnce.Do(func() {
		c.authService = service.NewAuthService(
			c.log,
			c.UserRepo(ctx),
			c.TokenRepo(ctx),
			c.Transactor(ctx),
			c.cfg.JWT.AccessTTL,
			c.cfg.JWT.RefreshTTL,
			c.secrets.JWTSecret,
		)
	})

	return c.authService
}

func (c *diContainer) GRPCConn() *grpc.ClientConn {
	c.grpcConnOnce.Do(func() {
		conn, err := grpc.NewClient(
			c.cfg.GRPC.Address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                c.cfg.GRPC.Keepalive.Time,
				Timeout:             c.cfg.GRPC.Keepalive.Timeout,
				PermitWithoutStream: c.cfg.GRPC.Keepalive.PermitWithoutStream,
			}),
		)
		if err != nil {
			c.log.Error("failed to initialize gRPC client", "err", err)
			os.Exit(1)
		}

		closer.Add("grpc client", func(_ context.Context) error {
			return conn.Close()
		})

		c.grpcConn = conn
	})

	return c.grpcConn
}

func (c *diContainer) SignalingService() *grpcsignaling.Service {
	c.signalingOnce.Do(func() {
		c.signalingSvc = grpcsignaling.New(c.log, c.GRPCConn(), grpcsignaling.Config{
			ReconnectMinDelay: c.cfg.Signaling.ReconnectMinDelay,
			ReconnectMaxDelay: c.cfg.Signaling.ReconnectMaxDelay,
		})
	})

	return c.signalingSvc
}

func (c *diContainer) Validator() *validator.Validate {
	c.validatorOnce.Do(func() {
		c.validator = validator.New(validator.WithRequiredStructEnabled())
	})

	return c.validator
}

func (c *diContainer) UserHandler(ctx context.Context) *handler.UserHandler {
	c.userHandlerOnce.Do(func() {
		c.userHandler = handler.NewUserHandler(c.log, c.AuthService(ctx), c.Validator())
	})

	return c.userHandler
}

func (c *diContainer) TokenHandler(ctx context.Context) *handler.TokenHandler {
	c.tokenHandlerOnce.Do(func() {
		c.tokenHandler = handler.NewTokenHandler(c.log, c.AuthService(ctx), c.Validator())
	})

	return c.tokenHandler
}

func (c *diContainer) WSHandler(ctx context.Context) *handler.WSHandler {
	c.wsHandlerOnce.Do(func() {
		c.wsHandler = handler.NewWSHandler(c.log, c.SignalingService(), handler.WSHandlerConfig{
			EnqueueTimeout:      c.cfg.Signaling.EnqueueTimeout,
			LeaveTimeout:        c.cfg.Signaling.LeaveTimeout,
			ControlWriteTimeout: c.cfg.Signaling.ControlWriteTimeout,
			SendBufferSize:      c.cfg.Signaling.WebSocketSendBufSize,
		})
	})

	return c.wsHandler
}

func (c *diContainer) Router(ctx context.Context) *chi.Mux {
	c.routerOnce.Do(func() {
		router := chi.NewRouter()

		router.Use(middleware.RequestID)
		router.Use(middleware.RealIP)
		router.Use(middleware.Recoverer)
		router.Use(middleware.URLFormat)

		router.Route("/api", func(r chi.Router) {
			r.Route("/user", func(r chi.Router) {
				r.Post("/register", c.UserHandler(ctx).Register)
				r.Post("/login", c.UserHandler(ctx).Login)
			})
			r.Route("/token", func(r chi.Router) {
				r.Post("/refresh", c.TokenHandler(ctx).Refresh)
			})
			r.Route("/ws", func(r chi.Router) {
				r.Use(mw.AuthMiddleware(c.secrets.JWTSecret))
				r.Get("/", c.WSHandler(ctx).Handle)
			})
		})

		c.router = router
	})

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
