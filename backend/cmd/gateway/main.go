package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"github.com/wangn-tech/speak-up/internal/app"
	"github.com/wangn-tech/speak-up/internal/database"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	config := app.ConfigFromEnv()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sqlx.ConnectContext(ctx, "mysql", config.MySQLDSN)
	if err != nil {
		slog.Error("connect mysql", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Error("close mysql", "error", closeErr)
		}
	}()
	if err = database.Migrate(ctx, db); err != nil {
		slog.Error("migrate mysql", "error", err)
		os.Exit(1)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: config.RedisAddress})
	defer func() {
		if closeErr := redisClient.Close(); closeErr != nil {
			slog.Error("close redis", "error", closeErr)
		}
	}()
	if err = redisClient.Ping(ctx).Err(); err != nil {
		slog.Error("connect redis", "error", err)
		os.Exit(1)
	}

	aiConn, err := grpc.NewClient(config.AIAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("configure ai grpc", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := aiConn.Close(); closeErr != nil {
			slog.Error("close ai grpc", "error", closeErr)
		}
	}()

	store := app.NewMySQLStore(db)
	go app.RunOutboxPublisher(ctx, store, config.KafkaBrokers)
	go app.RunEvaluationConsumer(ctx, store, config.KafkaBrokers)

	server := &http.Server{Addr: config.Address, Handler: app.NewServer(config, store, aiConn).Router(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("gateway listening", "address", config.Address)
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error("serve gateway", "error", serveErr)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown gateway", "error", err)
	}
}
