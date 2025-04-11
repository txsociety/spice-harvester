package indexer

import (
	"context"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"sync"
	"time"
)

type Indexer struct {
	blockchain blockchain
	storage    storage
	accounts   chan core.Account
}

func New(blockchain blockchain, storage storage) (*Indexer, error) {
	accountsChan := make(chan core.Account)
	processor := &Indexer{
		blockchain: blockchain,
		storage:    storage,
		accounts:   accountsChan,
	}
	return processor, nil
}

func (i *Indexer) Run(ctx context.Context, wg *sync.WaitGroup) chan core.Account {
	go i.runExpirationProcessor(ctx, wg)
	go i.runIndexer(ctx, wg)
	return i.accounts
}

func (i *Indexer) runExpirationProcessor(ctx context.Context, wg *sync.WaitGroup) {
	slog.Info("expiration processor started")
	wg.Add(1)
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			slog.Info("expiration processor stopped")
			return
		case <-time.After(5 * time.Second):
			ctx1, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := i.storage.DeleteExpiredKeys(ctx1)
			if err != nil {
				slog.Error("failed to delete expired keys", "err", err)
				cancel()
				continue
			}
			err = i.storage.MarkExpired(ctx1)
			cancel()
			if err != nil {
				slog.Error("failed to mark expired invoices", "err", err)
				continue
			}
		}
	}
}

func (i *Indexer) runIndexer(ctx context.Context, wg *sync.WaitGroup) {
	slog.Info("indexer started")
	wg.Add(1)
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			// TODO: stop all workers
			slog.Info("indexer stopped")
			return
		case acc, ok := <-i.accounts:
			if !ok {
				slog.Error("account channel closed")
				return
			}
			err := i.trackAccount(acc)
			if err != nil {
				slog.Error("failed to start track account", "err", err, "address", acc.AccountID.ToRaw())
			}
		}
	}
}

func (i *Indexer) trackAccount(account core.Account) error {
	// TODO: map workers
	loader := newLoaderWorker(account, i.blockchain, i.storage)
	idx, err := newIndexerWorker(i.storage, account)
	if err != nil {
		return err
	}
	go loader.Run()
	go idx.Run()
	return nil
}
