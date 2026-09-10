// 进程入口：连维护库、按工厂身份打开厂库，对外提供账号 HTTP。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wmesh/factory/internal/httpapi"
	"wmesh/factory/internal/hub"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := os.Getenv("WMESH_DSN")
	if dsn == "" {
		dsn = "postgres://wmesh:wmesh@127.0.0.1:55433/postgres?sslmode=disable"
	}
	bootToken := os.Getenv("WMESH_BOOTSTRAP_TOKEN")
	if bootToken == "" {
		return fmt.Errorf("WMESH_BOOTSTRAP_TOKEN required")
	}
	h, err := hub.New(dsn)
	if err != nil {
		return err
	}
	defer h.Close()
	addr := os.Getenv("WMESH_HTTP_ADDR")
	if addr == "" {
		addr = ":8081"
	}
	srv := &http.Server{Addr: addr, Handler: httpapi.New(h, bootToken).Router()}
	go func() {
		log.Printf("factory http %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func wait() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}
