// Command server 是控制面入口（API 与 Runner 同进程，见 dev_spec 5.1）。
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"github.com/roarzx/my-aladin/internal/activity"
	"github.com/roarzx/my-aladin/internal/auth"
	"github.com/roarzx/my-aladin/internal/buildinfo"
	"github.com/roarzx/my-aladin/internal/config"
	"github.com/roarzx/my-aladin/internal/domain"
	"github.com/roarzx/my-aladin/internal/flow"
	"github.com/roarzx/my-aladin/internal/health"
	"github.com/roarzx/my-aladin/internal/httpapi"
	"github.com/roarzx/my-aladin/internal/machine"
	"github.com/roarzx/my-aladin/internal/orchestration"
	"github.com/roarzx/my-aladin/internal/plan"
	"github.com/roarzx/my-aladin/internal/recovery"
	"github.com/roarzx/my-aladin/internal/session"
	mysqlstore "github.com/roarzx/my-aladin/internal/store/mysql"
	redisstore "github.com/roarzx/my-aladin/internal/store/redis"
	"github.com/roarzx/my-aladin/internal/task"
	"github.com/roarzx/my-aladin/internal/transport/ws"
	"github.com/roarzx/my-aladin/migrations"
	"github.com/roarzx/my-aladin/pkg/errcode"
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

	healthHandler := func(w http.ResponseWriter, _ *http.Request) {
		httpapi.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	readyHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		details := checker.Details(ctx)
		if len(details) > 0 {
			httpapi.JSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "unavailable",
				"errors": details,
			})
			return
		}
		httpapi.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}

	authenticator := auth.StaticToken{
		Token:  os.Getenv("ALADDIN_ADMIN_TOKEN"),
		UserID: cfg.Security.AdminUserID,
	}
	if authenticator.Token == "" {
		slog.Warn("ALADDIN_ADMIN_TOKEN 未设置：业务接口将全部返回 401")
	}

	hub := ws.NewHub(slog.Default())
	lease := &orchestration.Lease{
		DB:    db,
		Store: redisstore.NewLeaseStore(rdb),
		TTL:   cfg.Scheduler.AttemptLeaseTTL.Std(),
		Log:   slog.Default(),
	}
	taskLock := orchestration.NewTaskLock(rdb,
		cfg.Scheduler.TaskLockTTL.Std(), cfg.Scheduler.TaskLockRenew.Std())
	runner := &orchestration.PlanRunner{
		DB: db, Runtime: hub, Owner: "server",
		Lock:  taskLock,
		Lease: lease,
		Log:   slog.Default(),
	}
	eventHandler := &orchestration.EventHandler{DB: db, Runner: runner, Log: slog.Default()}
	hub.OnEvent = eventHandler.Handle
	hub.Lease = lease
	canceller := &orchestration.Canceller{DB: db, Runtime: hub, Log: slog.Default()}
	reconciler := &recovery.Reconciler{DB: db, Handlers: eventHandler}

	taskService := &task.Service{DB: db}
	sessionService := &session.Service{DB: db, Dispatcher: hub}
	squadRunner := &orchestration.SquadRunner{
		DB: db, Sessions: sessionService, Lock: taskLock,
		Owner: "server-squad", Log: slog.Default(),
	}
	compositeRunner := &orchestration.CompositeRunner{
		DB: db, Plan: runner, Squad: squadRunner,
	}
	taskService.OnCreated = func(ctx context.Context, created *domain.Task) {
		if err := sessionService.DispatchPlanning(ctx, created.ID, ""); err != nil {
			slog.Warn("创建规划会话失败", "task", created.ID, "err", err)
		}
	}
	planService := &plan.Service{DB: db}
	retryService := &orchestration.RetryService{
		DB: db, Runtime: hub, MaxAttempts: cfg.Scheduler.MaxAttemptsPerStep,
	}
	retryHandler := &orchestration.Handler{
		DB: db, Members: &mysqlstore.WorkspaceStore{DB: db}, Service: retryService,
	}
	activityHandler := &activity.Handler{DB: db, Members: &mysqlstore.WorkspaceStore{DB: db}}
	taskHandler := &task.Handler{Service: taskService, DB: db, Members: &mysqlstore.WorkspaceStore{DB: db}}
	cancelHandler := &orchestration.CancelHandler{
		Members: &mysqlstore.WorkspaceStore{DB: db}, Canceller: canceller,
	}
	reconcileHandler := &recovery.Handler{
		DB: db, Members: &mysqlstore.WorkspaceStore{DB: db}, Reconciler: reconciler,
	}
	planHandler := &plan.Handler{
		DB:      db,
		Members: &mysqlstore.WorkspaceStore{DB: db},
		Service: planService,
	}

	machineAuthenticator := func(req *http.Request) (string, error) {
		header := req.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
			return "", errors.New("缺少机器令牌")
		}
		machineID, err := mysqlstore.MachineByToken(req.Context(), db, machine.HashToken(header[len(prefix):]))
		if err != nil {
			return "", err
		}
		return machineID, nil
	}
	wsServer := ws.NewServer(machineAuthenticator, ws.Config{
		HeartbeatInterval: cfg.Server.HeartbeatInterval.Std(),
		HeartbeatTimeout:  cfg.Server.HeartbeatTimeout.Std(),
		MaxMessageBytes:   cfg.ACP.MaxMessageBytes,
	}, slog.Default())
	wsServer.Handler = hub.Handler

	router := httpapi.New(httpapi.Deps{
		Authenticator: authenticator,
		Members:       &mysqlstore.WorkspaceStore{DB: db},
		Logger:        slog.Default(),
		Health:        healthHandler,
		Ready:         readyHandler,
		MountRoot: func(r chi.Router) {
			r.Handle(cfg.Server.WSPath, wsServer)
			r.Route("/api/v1/cli", func(r chi.Router) {
				r.Use(httpapi.MachineAuth(machineAuthenticator))
				httpapi.MountCLI(r, &contextStore{db: db})
				flowHandler := &flow.Handler{DB: db, TTL: cfg.Scheduler.AttemptLeaseTTL.Std()}
				r.Post("/flow-run-nodes/claim", flowHandler.Claim)
				r.Post("/flow-run-nodes/{id}/heartbeat", flowHandler.Heartbeat)
				r.Post("/flow-run-nodes/{id}/complete", flowHandler.Complete)
				r.Post("/flow-run-nodes/{id}/fail", flowHandler.Fail)
				r.Post("/flow-run-nodes/{id}/cancel-ack", flowHandler.CancelAck)
			})
		},
		RegisterGlobal: func(r chi.Router) {
			r.Get("/issues", taskHandler.List)
			r.Get("/issues/{id}", taskHandler.Get)
			r.Get("/issues/{id}/plans/{rev}", planHandler.Get)
			r.Post("/issues/{id}/plans", planHandler.Submit)
			r.Post("/issues/{id}/cancel", cancelHandler.Cancel)
			r.Post("/steps/{id}/reconcile", reconcileHandler.Reconcile)
			r.Post("/plans/{id}/confirm", planHandler.Confirm)
			r.Post("/issues/{id}/start", planHandler.Start)
			r.Post("/steps/{id}/retry", retryHandler.Retry)
			r.Get("/issues/{id}/events", activityHandler.List)
		},
		RegisterWorkspace: func(r chi.Router) {
			r.With(auth.RequireAction(auth.ActionCreateIssue, nil)).
				Post("/issues", (&task.Handler{Service: taskService}).Create)
		},
	})

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()
	go (&orchestration.Loop{DB: db, Runner: compositeRunner, Interval: time.Second, Log: slog.Default()}).Run(loopCtx)
	go (&recovery.Watchdog{
		DB: db, Runner: compositeRunner, Lease: lease, Canceller: canceller,
		CancelTimeout: cfg.Scheduler.CancelTimeout.Std(),
		Interval:      cfg.Scheduler.RecoveryScanInterval.Std(),
		Log:           slog.Default(),
	}).Run(loopCtx)

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

// contextStore 实现 httpapi.ContextStore，供 daemon 的 CLI HTTP API 使用（9.6.1）。
type contextStore struct{ db *sql.DB }

func (s *contextStore) ListVersions(ctx context.Context, taskID, scopeKind, scopeID string) ([]httpapi.ContextVersion, error) {
	entries, err := mysqlstore.ListContextVersions(ctx, s.db, taskID, scopeKind, scopeID)
	if err != nil {
		return nil, err
	}
	out := make([]httpapi.ContextVersion, 0, len(entries))
	for _, entry := range entries {
		out = append(out, httpapi.ContextVersion{Kind: entry.Kind, Version: entry.Version})
	}
	return out, nil
}

func (s *contextStore) GetVersion(ctx context.Context, taskID, scopeKind, scopeID, kind string, version int) (string, error) {
	entry, err := mysqlstore.GetContextVersion(ctx, s.db, taskID, scopeKind, scopeID, kind, version)
	if err != nil {
		return "", err
	}
	return entry.Content, nil
}

func (s *contextStore) PutVersion(ctx context.Context, taskID, scopeKind, scopeID, kind, content string, expectedVersion int) (int, error) {
	entry, err := mysqlstore.PutContextVersion(ctx, s.db, taskID, scopeKind, scopeID, kind, content, expectedVersion)
	if errors.Is(err, mysqlstore.ErrContextConflict) {
		return 0, errcode.New(errcode.Conflict, "上下文版本冲突")
	}
	if err != nil {
		return 0, err
	}
	return entry.Version, nil
}
