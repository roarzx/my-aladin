// Command server 是控制面入口（API 与 Runner 同进程，见 dev_spec 5.1）。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"github.com/roarzx/my-aladin/internal/buildinfo"
	"github.com/roarzx/my-aladin/internal/config"
	"github.com/roarzx/my-aladin/internal/health"
	"github.com/roarzx/my-aladin/migrations"
)

func main() {
	var (
		cfgPath    string
		healthOnly bool
	)
	flag.StringVar(&cfgPath, "config", "config/config.yaml", "配置文件路径")
	flag.BoolVar(&healthOnly, "health", false, "只做二进制自检并退出（不连数据库）")
	flag.Parse()

	if healthOnly {
		fmt.Printf("server: ok (%s)\n", buildinfo.String())
		return
	}

	if err := run(cfgPath); err != nil {
		slog.Error("server 退出", "err", err)
		os.Exit(1)
	}
}

func run(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	db, err := sql.Open("mysql", cfg.Storage.MySQLDSN)
	if err != nil {
		return fmt.Errorf("打开 MySQL: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Storage.RedisAddr})
	defer func() { _ = rdb.Close() }()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPing()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("MySQL 不可达: %w", err)
	}
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return fmt.Errorf("redis 不可达: %w", err)
	}

	checker := health.New(
		health.MySQL(db, migrations.LatestVersion()),
		health.Redis(rdb),
	)

	router := chi.NewRouter()
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		details := checker.Details(ctx)
		if len(details) > 0 {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "unavailable",
				"errors": details,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	srv := &http.Server{
		Addr:              cfg.Server.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server 监听中",
			"addr", cfg.Server.HTTPAddr,
			"version", buildinfo.String(),
			"ws_path", cfg.Server.WSPath,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		slog.Info("收到退出信号", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
