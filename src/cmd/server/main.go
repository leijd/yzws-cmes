package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clothp/internal/auth"
	"clothp/internal/config"
	"clothp/internal/database"
	"clothp/internal/httpapi"
	"clothp/internal/masterdata"
	"clothp/internal/platformadmin"
	"clothp/internal/production"
	"clothp/internal/tenantreg"
)

func main() {
	configPath := flag.String("config", "", "指定配置文件路径（该文件中的配置优先于环境变量；也可用 CLOTHP_CONFIG 环境变量指定）")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		log.Fatal(err)
	}
	authService := auth.NewService(db, cfg.JWTSecret, cfg.NodeID, cfg.AccessTokenTTL, cfg.OfflineSessionTTL)
	if err := authService.Bootstrap(ctx, cfg.BootstrapAdminPassword); err != nil {
		log.Fatal(err)
	}
	if err := authService.BootstrapPlatformAdmin(ctx, cfg.BootstrapPlatformAdminPassword); err != nil {
		log.Fatal(err)
	}
	api := httpapi.New(db, authService, cfg)
	api.RegisterAuthRoutes()
	api.RegisterTrialRoutes()
	api.RegisterLoginQRRoutes()
	api.RegisterTrialAuthRoutes()
	masterdata.Register(api)
	production.Register(api)
	platformadmin.Register(api)
	tenantreg.Register(api)

	server := &http.Server{Addr: cfg.Listen, Handler: api.Handler(), ReadHeaderTimeout: cfg.ReadHeaderTimeout}
	go func() {
		log.Printf("clothp %s listening on %s", cfg.Mode, cfg.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
