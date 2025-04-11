package indexer

import (
	"context"
	"fmt"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"time"
)

type loaderWorker struct {
	account            ton.AccountID
	blockchain         blockchain
	storage            storage
	lastLt, maxDepthLt uint64
}

func newLoaderWorker(a core.Account, blockchain blockchain, storage storage) *loaderWorker {
	w := loaderWorker{
		account:    a.AccountID,
		blockchain: blockchain,
		storage:    storage,
		maxDepthLt: a.Info.MaxDepthLt,
	}
	return &w
}

func (w *loaderWorker) Run() {
	gaps, lastLt, err := w.storage.GetGaps(context.Background(), w.account)
	if err != nil {
		time.Sleep(time.Minute) // maybe database is unavailable
		gaps, lastLt, err = w.storage.GetGaps(context.Background(), w.account)
		if err != nil {
			panic(err) //it's mean some problem in database. we can't work without database
		}
	}
	w.lastLt = lastLt
	for _, gap := range gaps {
		err := w.syncHistoryGap(gap.StartHash, gap.StartLt, gap.EndLt)
		if err != nil {
			panic(err) //it's mean some problem in database. we can't work without database
		}
	}
	for {
		err := w.refreshAccount()
		if err != nil {
			slog.Error("refresh account", "error", err.Error())
		}
		time.Sleep(time.Second * 5)
	}
}

func (w *loaderWorker) syncHistoryGap(startHash ton.Bits256, startLt, endLt uint64) error {
	if endLt < w.maxDepthLt { // sync only up to maxDepthLt TODO: optimize
		endLt = w.maxDepthLt
	}
	startLtCopy := startLt
	for {
		if startLt == endLt {
			return nil
		}
		nextHash, nextLt, err := w.syncGapIteration(startHash, startLt, endLt)
		if err != nil {
			return err
		}
		if nextLt <= endLt {
			break
		}
		startHash = nextHash
		startLt = nextLt
	}
	w.lastLt = startLtCopy
	return nil
}

func (w *loaderWorker) syncGapIteration(startHash ton.Bits256, startLt, endLt uint64) (nextHash ton.Bits256, nextLt uint64, err error) {
	var txs []core.Transaction
	for i := range 200 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		txs, err = w.blockchain.GetTransactions(ctx, w.account, startLt, endLt, startHash)
		cancel()
		if err == nil && len(txs) > 0 {
			break
		}
		slog.Error("get transactions", "error", err)
		slog.Info("retry", "sleep seconds", i)
		time.Sleep(time.Second * time.Duration(i))
	}
	if err != nil {
		return ton.Bits256{}, 0, fmt.Errorf("get transactions: %w", err)
	}
	if len(txs) == 0 {
		return ton.Bits256{}, 0, fmt.Errorf("no transactions for %v %v %v %v", w.account.String(), startLt, endLt, time.Now())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = w.storage.SaveTransactions(ctx, w.account, txs)
	if err != nil {
		return ton.Bits256{}, 0, fmt.Errorf("save transactions: %w", err)
	}
	nextHash = txs[len(txs)-1].PrevTxHash
	nextLt = txs[len(txs)-1].PrevTxLt
	return nextHash, nextLt, nil
}

func (w *loaderWorker) refreshAccount() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	account, blockSeqno, err := w.blockchain.GetAccountState(ctx, w.account)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}
	if account.LastTransLt < w.lastLt {
		return fmt.Errorf("account has older state than previous: %v %v (probably unsinc between nodes)", account.LastTransLt, w.lastLt)
	}
	txID := core.TxID{
		Lt:   account.LastTransLt,
		Hash: ton.Bits256(account.LastTransHash),
	}
	err = w.storage.UpdateAccount(ctx, w.account, txID, blockSeqno)
	if err != nil {
		return fmt.Errorf("save account: %w", err)
	}
	err = w.syncHistoryGap(ton.Bits256(account.LastTransHash), account.LastTransLt, w.lastLt)
	if err != nil {
		panic(err) //yes, panic. better to fall down
	}
	return nil
}
