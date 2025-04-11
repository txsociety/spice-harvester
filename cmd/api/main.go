package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/tonkeeper/tongo/liteclient"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/internal/config"
	"github.com/txsociety/spice-harvester/pkg/api"
	"github.com/txsociety/spice-harvester/pkg/blockchain"
	"github.com/txsociety/spice-harvester/pkg/core"
	"github.com/txsociety/spice-harvester/pkg/db"
	"github.com/txsociety/spice-harvester/pkg/indexer"
	"github.com/txsociety/spice-harvester/pkg/notifier"
	"github.com/txsociety/spice-harvester/pkg/webhook"
	"golang.org/x/crypto/ed25519"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var Version = "dev"

func main() {
	cfg := config.Load()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})))
	slog.Info("running invoice processor", "version", Version, "log level", cfg.LogLevel.String())

	var (
		adnlAddr         *ton.Bits256
		ourEncryptionKey ed25519.PrivateKey
	)
	if len(cfg.Key) > 0 {
		a, err := core.GetAdnlAddress(cfg.Key)
		if err != nil {
			slog.Error("calculating ADNL address", "error", err)
			os.Exit(1)
		}
		adnlAddr = &a
		printableAddr := liteclient.ADNLAddressToBase32(a)
		slog.Info("calculating ADNL address", "address", printableAddr+".adnl")
		ourEncryptionKey, err = core.GetEncryptionKey(cfg.Key)
		if err != nil {
			slog.Error("get encryption key", "error", err)
			os.Exit(1)
		}
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	wg := new(sync.WaitGroup)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	dbClient, err := db.New(ctx, cfg.PostgresURI, cfg.Recipient)
	if err != nil {
		slog.Error("db connection", "error", err)
		os.Exit(1)
	}
	err = dbClient.SaveCurrencies(ctx, cfg.Currencies)
	if err != nil {
		slog.Error("save currencies", "error", err)
		os.Exit(1)
	}
	cancel()

	ctx, cancel = context.WithCancel(context.Background())

	var wh *webhook.Client
	if len(cfg.WebhookEndpoint) > 0 {
		wh, err = webhook.NewClient(cfg.WebhookEndpoint)
		if err != nil {
			slog.Error("webhook connection", "error", err)
			os.Exit(1)
		}
	}

	bcClient, err := blockchain.New(cfg.LiteServers)
	if err != nil {
		slog.Error("blockchain connection", "error", err)
		os.Exit(1)
	}
	bcClient.RunBlockWatcher(ctx, dbClient, wg)

	indexerProc, err := indexer.New(bcClient, dbClient)
	if err != nil {
		slog.Error("processor creation", "error", err)
		os.Exit(1)
	}

	var notifierProc *notifier.Notifier
	if wh != nil {
		notifierProc = notifier.New(wh, cfg.Currencies, adnlAddr, cfg.PaymentPrefixes, dbClient)
	} else {
		notifierProc = notifier.New(nil, cfg.Currencies, adnlAddr, cfg.PaymentPrefixes, dbClient)
	}

	accountsChan := indexerProc.Run(ctx, wg)
	notifierProc.Run(ctx, wg)

	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	accounts, err := getAccountsForTracking(ctx1, dbClient, bcClient, cfg.Recipient, cfg.Currencies)
	cancel1()
	if err != nil {
		slog.Error("get accounts for tracking", "error", err)
		os.Exit(1)
	}
	for acc, info := range accounts {
		accountsChan <- core.Account{
			AccountID: acc,
			Info:      info,
		}
	}

	mux := http.NewServeMux()
	handler := api.NewHandler(dbClient, cfg.Currencies, adnlAddr, cfg.PaymentPrefixes, ourEncryptionKey)
	api.RegisterHandlers(mux, handler, cfg.Token)
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%v", cfg.Port),
		Handler: mux,
	}
	go func() {
		slog.Info("running api server", "port", cfg.Port)
		err = srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen and serve", "error", err)
			os.Exit(1)
		}
	}()

	sig := <-ch
	slog.Info("shut down", "signal", sig.String())
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown", "error", err)
	}
	slog.Info("api stopped")
	cancel()
	wg.Wait()
}
