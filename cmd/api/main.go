package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/wilfex81/rate-limiter/limiter"
	"github.com/wilfex81/trips-api/internal/auth"
	"github.com/wilfex81/trips-api/internal/config"
	"github.com/wilfex81/trips-api/internal/httpapi"
	"github.com/wilfex81/trips-api/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}

	defer pool.Close()

	if err := store.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}
	tokens, err := auth.NewService(cfg.JWTSecret, cfg.TokenTTL, bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 5 attempts at once, then one every 10 seconds, per client address
	ipLimiter, err := newBucketStore(cfg.AuthBurst, cfg.AuthRate)
	if err != nil {
		return err
	}
	defer ipLimiter.Close()

	// 60 requests at once, then one per second, per logged in user
	userLimiter, err := newBucketStore(cfg.APIBurst, cfg.APIRate)
	if err != nil {
		return err
	}
	defer userLimiter.Close()

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			DB:             pool,
			Drivers:        store.NewDriverStore(pool),
			Users:          store.NewUserStore(pool),
			Auth:           tokens,
			Trips:          store.NewTripStore(pool),
			IPLimiter:      ipLimiter,
			UserLimiter:    userLimiter,
			TrustedProxies: cfg.TrustedProxies,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCTX, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		srv.Shutdown(shutdownCTX)
	}()

	log.Printf("listening on %s", cfg.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Println("shutdown complete")
	return nil

}

// newBucketStore keeps one token bucket per client key.
func newBucketStore(capacity int, ratePerSec float64) (*limiter.Store, error) {
	// check the numbers once, so the factory below cannot fail
	if _, err := limiter.NewTokenBucket(capacity, ratePerSec); err != nil {
		return nil, err
	}
	// a client must be idle longer than a bucket needs to refill, or it could
	// reset its own limit by going quiet, so add a minute of margin
	idle := time.Duration(float64(capacity)/ratePerSec*float64(time.Second)) + time.Minute
	return limiter.NewStore(func() limiter.Limiter {
		l, _ := limiter.NewTokenBucket(capacity, ratePerSec)
		return l
	}, idle, time.Minute), nil
}
