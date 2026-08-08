package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lexa044/realtime-api/internal/adapter/broker"
	httpadapter "github.com/lexa044/realtime-api/internal/adapter/http"
	"github.com/lexa044/realtime-api/internal/adapter/repository"
	"github.com/lexa044/realtime-api/internal/adapter/ws"
	"github.com/lexa044/realtime-api/internal/infrastructure/config"
	infradb "github.com/lexa044/realtime-api/internal/infrastructure/db"
	"github.com/lexa044/realtime-api/internal/usecase"
)

func main() {
	// --- Load configuration ---
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startupCancel()

	db, err := infradb.NewMSSQL(startupCtx, cfg.MSSQLDSN)
	if err != nil {
		log.Fatalf("mssql: %v", err)
	}
	defer db.Close()

	rdb, err := infradb.NewRedis(startupCtx, cfg.RedisAddr, cfg.RedisPass)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	// Wiring: adapters -> usecases -> handlers. Dependencies point inward;
	// this is the ONLY place that knows about every concrete type.
	orderRepo := repository.NewOrderRepository(db)
	publisher := broker.NewPublisher(rdb)
	orderService := usecase.NewOrderService(orderRepo, publisher)
	orderHandler := httpadapter.NewOrderHandler(orderService)

	hub := ws.NewHub()
	go hub.Run()

	subscriber := broker.NewSubscriber(rdb, hub, usecase.OrderEventsChannel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := subscriber.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("redis subscriber stopped: %v", err)
		}
	}()

	router := httpadapter.NewRouter(orderHandler, hub, httpadapter.AuthMiddleware([]byte(cfg.JWTSecret)))

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	go func() {
		log.Printf("listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("shutting down...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
}
