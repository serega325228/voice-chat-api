package grpcapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"voice-chat-api/internal/grpc/signaling"

	"google.golang.org/grpc"
)

type App struct {
	log        *slog.Logger
	gRPCClient *grpc.ClientConn
	port       int
}

func New(
	log *slog.Logger,
	port int,
	signalingService signaling.SignalingService,
) (*App, error) {
	//TODO
	gRPCClient, err := grpc.NewClient(
		"localhost:80802",
		//TODO
		grpc.EmptyDialOption{},
	)
	//TODO
	if err != nil {
		return nil, err
	}
	session.Register(gRPCClient, sessionService)

	return &App{
		log:        log,
		gRPCClient: gRPCClient,
		port:       port,
	}, nil
}

func (a *App) Run() error {
	const op = "grpcapp.App.Run"

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", a.port))
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	a.log.Info("gRPC server listening", "port", a.port)

	if err := a.gRPCClient.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (a *App) Close(ctx context.Context) error {
	const op = "grpcapp.App.Close"

	done := make(chan struct{})

	go func() {
		a.gRPCClient.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		a.log.Warn("graceful shutdown timed out, forcing stop")
		a.gRPCClient.Stop()
		<-done
		return fmt.Errorf("%s: %w", op, ctx.Err())
	}
}
