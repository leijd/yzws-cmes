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
	"clothp/internal/datastore"
	"clothp/internal/datatool"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/orgapp"
	"clothp/internal/person"
	"clothp/internal/seccrypto"
	"clothp/internal/masterdata"
	"clothp/internal/platformadmin"
	"clothp/internal/production"
	"clothp/internal/tenantreg"
)

func main() {
	configPath := flag.String("config", "", "指定配置文件路径（该文件中的配置优先于环境变量；也可用 CLOTHP_CONFIG 环境变量指定）")
	datatoolCmd := flag.String("datatool", "", "数据互转工具：convert=全量转换；repair-phone-hash=修复手机号盲索引；repair-permission-divergence=修复两库权限点/授权主键分叉；align-subscription-keys=对齐订阅表业务键分叉；rewrap-local-master=用新口令重包租户密钥（读 CLOTHP_NEW_JWT_SECRET）；执行后退出，需配置 databases.secure")
	datatoolDirection := flag.String("direction", "plain2secure", "convert 方向：plain2secure（明文→密文）| secure2plain（密文→明文）")
	datatoolDryRun := flag.Bool("dry-run", true, "convert/repair-phone-hash 仅预检不落库；确认行数后用 -dry-run=false 执行")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	store, err := datastore.Open(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	/* 两库分别迁移并校验版本一致；both 模式还需 max_prepared_transactions>0 */
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	db := store.ReadPool()

	/* 密钥提供方与业务加密服务（设计 3.2）：提供方不可用只告警不回退，
	 * 涉及密文的读写由 seccrypto.Service 返回 KEY_UNAVAILABLE。local 仅用于解包旧记录迁移。 */
	provider, err := seccrypto.NewProvider(seccrypto.KMSProviderConfig{
		Provider:  cfg.Crypto.KMSProvider,
		JWTSecret: cfg.JWTSecret,
		SoftHSM2: seccrypto.SoftHSM2Settings{
			PKCS11ModuleEnv: cfg.Crypto.SoftHSM2.PKCS11ModuleEnv,
			TokenLabel:      cfg.Crypto.SoftHSM2.TokenLabel,
			PINEnv:          cfg.Crypto.SoftHSM2.PINEnv,
			KeyLabelPrefix:  cfg.Crypto.SoftHSM2.KeyLabelPrefix,
			ServerURL:       cfg.Crypto.SoftHSM2.ServerURL,
			APIKeyEnv:       cfg.Crypto.SoftHSM2.APIKeyEnv,
			CACertFile:      cfg.Crypto.SoftHSM2.CACertFile,
			TimeoutMS:       cfg.Crypto.SoftHSM2.TimeoutMS,
			SSHTarget:       cfg.Crypto.SoftHSM2.SSHTarget,
			ProvisionScript: cfg.Crypto.SoftHSM2.ProvisionScript,
		},
		CloudKMS: seccrypto.CloudKMSSettings{
			Provider:    cfg.Crypto.CloudKMS.Provider,
			Region:      cfg.Crypto.CloudKMS.Region,
			KeyIDPrefix: cfg.Crypto.CloudKMS.KeyIDPrefix,
		},
	})
	if err != nil {
		log.Printf("crypto provider unavailable: %v (encrypted field access will return KEY_UNAVAILABLE)", err)
		provider = nil
	}
	/* local 兼容适配器始终创建：用于解包 local-master:v1 旧记录与过渡阶段发行密钥；
	 * 非 local 提供方（softhsm2/cloud_kms）接入后，新密钥走当前提供方，local 仅承担历史解密。 */
	localProvider := seccrypto.NewLocalProvider(cfg.JWTSecret)
	cryptoSvc := seccrypto.NewService(provider, localProvider, dualwrite.NewDBKeyStore(store), 5*time.Minute)

	/* 数据互转工具（设计 3.8）：历史数据回补/反向迁移；执行后退出，不启动 HTTP 服务 */
	if *datatoolCmd == "convert" {
		result, err := datatool.Convert(ctx, store, cryptoSvc, *datatoolDirection, *datatoolDryRun, "cli:"+cfg.NodeID, "datatool convert")
		datatool.LogResult(result)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if *datatoolCmd == "repair-phone-hash" {
		result, err := datatool.RepairPhoneHash(ctx, store, cryptoSvc, *datatoolDryRun)
		datatool.LogResult(result)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if *datatoolCmd == "repair-permission-divergence" {
		result, err := datatool.RepairPermissionDivergence(ctx, store, *datatoolDryRun)
		datatool.LogResult(result)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if *datatoolCmd == "align-subscription-keys" {
		result, err := datatool.AlignSubscriptionKeys(ctx, store, *datatoolDryRun)
		datatool.LogResult(result)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if *datatoolCmd == "rewrap-local-master" {
		/* 新口令只从环境变量读取，避免出现在命令行历史/进程列表 */
		newSecret := os.Getenv("CLOTHP_NEW_JWT_SECRET")
		if newSecret == "" {
			log.Fatal("rewrap-local-master: set CLOTHP_NEW_JWT_SECRET to the new secret")
		}
		result, err := datatool.RewrapLocalMaster(ctx, store, cfg.JWTSecret, newSecret, *datatoolDryRun)
		datatool.LogResult(result)
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	/* 双写 2PC 协调器、崩溃恢复与明文→密文增量补偿循环（设计 3.3/3.8）；单库模式下 writer 仅单写明文库 */
	writer := dualwrite.NewWriter(store, cfg.NodeID)
	rowCrypto := dualwrite.NewRowCrypto(cryptoSvc)
	stopRecover := writer.StartRecoverLoop(ctx, time.Duration(cfg.DataStore.ReconcileIntervalSeconds)*time.Second, rowCrypto)
	defer stopRecover()
	_ = writer
	_ = cryptoSvc

	authService := auth.NewService(db, cfg.JWTSecret, cfg.NodeID, cfg.AccessTokenTTL, cfg.OfflineSessionTTL)
	if err := authService.Bootstrap(ctx, cfg.BootstrapAdminPassword); err != nil {
		log.Fatal(err)
	}
	if err := authService.BootstrapPlatformAdmin(ctx, cfg.BootstrapPlatformAdminPassword); err != nil {
		log.Fatal(err)
	}
	api := httpapi.New(db, authService, cfg)
	api.Store, api.Writer, api.Crypto = store, writer, cryptoSvc
	api.RegisterDataStoreRoutes(store)
	api.RegisterAccountLoginRoutes()
	api.RegisterPlainBackupRoutes()
	api.RegisterAuthRoutes()
	api.RegisterTrialRoutes()
	api.RegisterLoginQRRoutes()
	api.RegisterTrialAuthRoutes()
	masterdata.Register(api)
	production.Register(api)
	platformadmin.Register(api)
	tenantreg.Register(api)
	orgapp.Register(api)
	person.Register(api)

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
