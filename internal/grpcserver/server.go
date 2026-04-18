package grpcserver

import (
	"context"
	"net"

	"brook/config"
	"brook/internal/middleware"

	"github.com/Chandra179/gosdk/logger"
	"google.golang.org/grpc"
)

func Server(cfg *config.Config) {
	log := logger.NewLogger(cfg.Middleware.Logger.Level)

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.RequestIDUnaryInterceptor,
		),
	)

	// Register your gRPC service implementations here, e.g.:
	// pb.RegisterOrdersServer(srv, &ordersHandler{})

	lis, err := net.Listen("tcp", cfg.GRPC.Addr)
	if err != nil {
		log.Error(context.Background(), "grpc listen error", logger.Field{Key: "error", Value: err.Error()})
		return
	}
	log.Info(context.Background(), "grpc server starting", logger.Field{Key: "addr", Value: lis.Addr().String()})
	if err := srv.Serve(lis); err != nil {
		log.Error(context.Background(), "grpc server error", logger.Field{Key: "error", Value: err.Error()})
	}
}
