package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"github.com/redis/go-redis/v9"

	"github.com/lexa044/realtime-api/internal/adapter/broker"
	httpadapter "github.com/lexa044/realtime-api/internal/adapter/http"
	"github.com/lexa044/realtime-api/internal/adapter/repository"
	"github.com/lexa044/realtime-api/internal/adapter/ws"
	"github.com/lexa044/realtime-api/internal/infrastructure/config"
	"github.com/lexa044/realtime-api/internal/usecase"
)

func main() {
	// --- Load configuration ---
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	db, err := sql.Open("sqlserver", cfg.MSSQLDSN)
	if err != nil {
		log.Fatalf("mssql open: %v", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// sql.Open only validates DSN syntax — it does not connect. Ping here
	// so a bad host/port/credential fails fast at startup instead of on
	// the first request that happens to hit the database.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		log.Fatalf("mssql ping: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPass,
	})

	redisPingCtx, redisPingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer redisPingCancel()
	if err := rdb.Ping(redisPingCtx).Err(); err != nil {
		log.Fatalf("redis ping: %v", err)
	}

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
	db.Close()
	rdb.Close()
}

