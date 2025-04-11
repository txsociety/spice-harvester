package indexer

import (
	"context"
	"github.com/tonkeeper/tongo/tlb"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
)

type blockchain interface {
	GetTransactions(ctx context.Context, a ton.AccountID, lt, maxDepthLt uint64, hash ton.Bits256) ([]core.Transaction, error)
	GetAccountState(ctx context.Context, accountID ton.AccountID) (tlb.ShardAccount, uint32, error)
}

type storage interface {
	MarkExpired(ctx context.Context) error
	SavePayments(ctx context.Context, account ton.AccountID, txLt uint64, payments []core.Payment, err error) error
	UpdateAccount(ctx context.Context, account ton.AccountID, lastTX core.TxID, mcSeqno uint32) error
	DeleteExpiredKeys(ctx context.Context) error
	GetGaps(ctx context.Context, a ton.AccountID) ([]core.TxGap, uint64, error)
	SaveTransactions(ctx context.Context, a ton.AccountID, txs []core.Transaction) error
	LastProcessedLT(ctx context.Context, a ton.AccountID) (uint64, error)
	GetTransactionByParentLt(ctx context.Context, a ton.AccountID, lt uint64) (core.Transaction, error)
}
