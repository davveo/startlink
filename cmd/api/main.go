package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/starlink/push/internal/app/callback"
	"github.com/starlink/push/internal/app/campaign"
	apptpl "github.com/starlink/push/internal/app/template"
	"github.com/starlink/push/internal/bootstrap"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/handler"
	"github.com/starlink/push/internal/server"
)

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "config file path")
	flag.Parse()

	infra, err := bootstrap.NewInfra(*cfgPath)
	if err != nil {
		slog.Error("bootstrap failed", "err", err)
		os.Exit(1)
	}

	campaignSvc := campaign.NewService(campaign.Deps{
		Tasks:          infra.Tasks,
		PushRepo:       infra.Push,
		Cache:          infra.AggCache,
		Notifier:       infra.Webhook,
		Templates:      infra.Templates,
		Limiter:        infra.Limiter,
		BatchSize:      infra.Cfg.Scheduler.BatchSize,
		HighBizScenes:  infra.Cfg.MQ.HighBizScenes,
		DefaultChannel: domain.ChannelType(infra.Cfg.Campaign.DefaultChannel),
	})
	callbackSvc := callback.NewService(infra.Push, infra.Tasks)
	templateSvc := apptpl.NewService(infra.Templates)

	engine := server.New(infra.Cfg.Server.Mode, server.Deps{
		Campaign: handler.NewCampaignHandler(campaignSvc, infra.Channels),
		Callback: handler.NewCallbackHandler(callbackSvc),
		Template: handler.NewTemplateHandler(templateSvc),
	})

	srv := &http.Server{Addr: infra.Cfg.Server.Addr, Handler: engine}
	go func() {
		slog.Info("api server listening", "addr", infra.Cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	slog.Info("api server stopped")
}
