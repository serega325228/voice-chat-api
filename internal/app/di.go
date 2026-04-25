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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type diContainer struct {
	m            sync.Mutex
	cfg          *config.Config
	secrets      *config.Secrets
	log          *slog.Logger
	storage      *storage.Storage
	transactor   *storage.Transactor
	userRepo     *repo.UserRepo
	tokenRepo    *repo.TokenRepo
	authService  *service.AuthService
	grpcConn     *grpc.ClientConn
	signalingSvc *grpcsignaling.Service
	userHandler  *handler.UserHandler
	tokenHandler *handler.TokenHandler
	wsHandler    *handler.WSHandler
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
	c.m.Lock()
	if c.storage != nil {
		defer c.m.Unlock()
		return c.storage
	}
	c.m.Unlock()

	stg, err := storage.NewStorage(ctx, c.secrets, c.cfg)
	if err != nil {
		c.log.Error("failed to initialize storage", "err", err)
		os.Exit(1)
	}

	closer.Add("storage", func(_ context.Context) error {
		return stg.Close()
	})

	c.m.Lock()
	defer c.m.Unlock()
	if c.storage == nil {
		c.storage = stg
	}

	return c.storage
}

func (c *diContainer) Transactor(ctx context.Context) *storage.Transactor {
	c.m.Lock()
	if c.transactor != nil {
		defer c.m.Unlock()
		return c.transactor
	}
	c.m.Unlock()

	transactor := storage.NewTransactor(c.Storage(ctx))

	c.m.Lock()
	defer c.m.Unlock()
	if c.transactor == nil {
		c.transactor = transactor
	}

	return c.transactor
}

func (c *diContainer) UserRepo(ctx context.Context) *repo.UserRepo {
	c.m.Lock()
	if c.userRepo != nil {
		defer c.m.Unlock()
		return c.userRepo
	}
	c.m.Unlock()

	userRepo := repo.NewUserRepo(c.Transactor(ctx))

	c.m.Lock()
	defer c.m.Unlock()
	if c.userRepo == nil {
		c.userRepo = userRepo
	}

	return c.userRepo
}

func (c *diContainer) TokenRepo(ctx context.Context) *repo.TokenRepo {
	c.m.Lock()
	if c.tokenRepo != nil {
		defer c.m.Unlock()
		return c.tokenRepo
	}
	c.m.Unlock()

	tokenRepo := repo.NewTokenRepo(c.Transactor(ctx))

	c.m.Lock()
	defer c.m.Unlock()
	if c.tokenRepo == nil {
		c.tokenRepo = tokenRepo
	}

	return c.tokenRepo
}

func (c *diContainer) AuthService(ctx context.Context) *service.AuthService {
	c.m.Lock()
	if c.authService != nil {
		defer c.m.Unlock()
		return c.authService
	}
	c.m.Unlock()

	authService := service.NewAuthService(
		c.log,
		c.UserRepo(ctx),
		c.TokenRepo(ctx),
		c.Transactor(ctx),
		c.cfg.JWT.AccessTTL,
		c.cfg.JWT.RefreshTTL,
		c.secrets.JWTSecret,
	)

	c.m.Lock()
	defer c.m.Unlock()
	if c.authService == nil {
		c.authService = authService
	}

	return c.authService
}

func (c *diContainer) GRPCConn() *grpc.ClientConn {
	c.m.Lock()
	if c.grpcConn != nil {
		defer c.m.Unlock()
		return c.grpcConn
	}
	c.m.Unlock()

	conn, err := grpc.NewClient(
		c.cfg.GRPC.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		c.log.Error("failed to initialize gRPC client", "err", err)
		os.Exit(1)
	}

	closer.Add("grpc client", func(_ context.Context) error {
		return conn.Close()
	})

	c.m.Lock()
	defer c.m.Unlock()
	if c.grpcConn == nil {
		c.grpcConn = conn
	}

	return c.grpcConn
}

func (c *diContainer) SignalingService() *grpcsignaling.Service {
	c.m.Lock()
	if c.signalingSvc != nil {
		defer c.m.Unlock()
		return c.signalingSvc
	}
	c.m.Unlock()

	signalingSvc := grpcsignaling.New(c.log, c.GRPCConn())

	c.m.Lock()
	defer c.m.Unlock()
	if c.signalingSvc == nil {
		c.signalingSvc = signalingSvc
	}

	return c.signalingSvc
}

func (c *diContainer) UserHandler(ctx context.Context) *handler.UserHandler {
	c.m.Lock()
	if c.userHandler != nil {
		defer c.m.Unlock()
		return c.userHandler
	}
	c.m.Unlock()

	userHandler := handler.NewUserHandler(c.log, c.AuthService(ctx))

	c.m.Lock()
	defer c.m.Unlock()
	if c.userHandler == nil {
		c.userHandler = userHandler
	}

	return c.userHandler
}

func (c *diContainer) TokenHandler(ctx context.Context) *handler.TokenHandler {
	c.m.Lock()
	if c.tokenHandler != nil {
		defer c.m.Unlock()
		return c.tokenHandler
	}
	c.m.Unlock()

	tokenHandler := handler.NewTokenHandler(c.log, c.AuthService(ctx))

	c.m.Lock()
	defer c.m.Unlock()
	if c.tokenHandler == nil {
		c.tokenHandler = tokenHandler
	}

	return c.tokenHandler
}

func (c *diContainer) WSHandler(ctx context.Context) *handler.WSHandler {
	c.m.Lock()
	if c.wsHandler != nil {
		defer c.m.Unlock()
		return c.wsHandler
	}
	c.m.Unlock()

	wsHandler := handler.NewWSHandler(c.log, c.SignalingService())

	c.m.Lock()
	defer c.m.Unlock()
	if c.wsHandler == nil {
		c.wsHandler = wsHandler
	}

	return c.wsHandler
}

func (c *diContainer) Router(ctx context.Context) *chi.Mux {
	c.m.Lock()
	if c.router != nil {
		defer c.m.Unlock()
		return c.router
	}
	c.m.Unlock()

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

	c.m.Lock()
	defer c.m.Unlock()
	if c.router == nil {
		c.router = router
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
