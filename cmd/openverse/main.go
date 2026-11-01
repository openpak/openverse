// Command openverse serves verse.openpak.org.
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

	"github.com/openpak/openverse/internal/demo"
	"github.com/openpak/openverse/internal/miiverse"
	"github.com/openpak/openverse/internal/store"
	"github.com/openpak/openverse/internal/web"
)

func main() {
	cfg, err := web.ConfigFromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("openverse: %v", err)
	}
	var st store.Store
	if cfg.Demo {
		log.Printf("openverse: DEMO mode, fixture data and no real sign-in")
		st = demo.New()
	} else {
		st = miiverse.New(cfg.APIURL, cfg.APIKey)
	}
	site, err := web.New(cfg, st)
	if err != nil {
		log.Fatalf("openverse: %v", err)
	}
	srv := &http.Server{Addr: cfg.Addr, Handler: site, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	log.Printf("openverse: listening on %s", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("openverse: %v", err)
	}
}
