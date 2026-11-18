package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/panel"
	"github.com/ruiange/sing-box-panel/internal/store"
)

func main() {
	if len(os.Args) > 1 {
		os.Exit(runCommand(os.Args[1:]))
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("面板退出", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := panel.ConfigFromEnv()
	if err != nil {
		return err
	}
	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	service := panel.NewService(db, nil, cfg, logger)
	var managed *node.Node
	if cfg.Node != nil {
		managed, err = node.New(*cfg.Node)
		if err != nil {
			return err
		}
		defer managed.Close()
		service = panel.NewService(db, managed, cfg, logger)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	httpServer := &http.Server{
		Addr: cfg.Listen, Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 95 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	httpServer.RegisterOnShutdown(service.CloseLiveTraffic)
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	var serve func(net.Listener) error
	if cfg.IPHTTPS {
		var closeTLS func()
		serve, closeTLS, err = configureIPHTTPS(ctx, httpServer, cfg)
		if closeTLS != nil {
			defer closeTLS()
		}
	} else {
		serve, err = configureHTTPS(httpServer, cfg)
	}
	if err != nil {
		return err
	}
	initCtx, cancelInit := context.WithTimeout(ctx, 40*time.Second)
	err = service.Initialize(initCtx)
	cancelInit()
	if err != nil {
		return err
	}
	closeDomainAccess := service.StartDomainAccess(ctx, httpServer.Handler)
	defer closeDomainAccess()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("面板已启动", "listen", cfg.Listen, "https", cfg.HTTPSHost != "")
		serverErr <- serve(listener)
	}()
	var collectors sync.WaitGroup
	collectors.Go(func() { service.RunCollector(ctx) })
	if managed != nil && cfg.Node.RecordDestinations {
		collectors.Go(func() { service.RunDestinationCollector(ctx, managed) })
	}
	select {
	case err := <-serverErr:
		stop()
		collectors.Wait()
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	err = httpServer.Shutdown(shutdown)
	collectors.Wait()
	if err != nil {
		return err
	}
	lastSample, cancelSample := context.WithTimeout(shutdown, 5*time.Second)
	defer cancelSample()
	if err := service.CollectBeforeShutdown(lastSample); err != nil {
		logger.Warn("退出前流量采集失败", "error", err)
	}
	return nil
}
