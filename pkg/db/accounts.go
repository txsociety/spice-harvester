package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"time"
)

func (c *Connection) GetTrackedAccounts(ctx context.Context, recipient ton.AccountID, currencies map[string]core.Currency) (map[ton.AccountID]core.AccountInfo, error) {
	var jettons []string
	for _, cur := range currencies {
		if cur.Type == core.Jetton {
			jettons = append(jettons, cur.Jetton().ToRaw())
		}
	}
	rows, err := c.postgres.Query(ctx, `
		SELECT a.address, a.start_tx_lt, c.info
		FROM payments.jetton_wallets as jw
		LEFT JOIN payments.currencies as c ON c.id = jw.currency
		LEFT JOIN blockchain.accounts as a ON a.address = jw.address
		WHERE jw.owner = $1 and info = ANY($2)`, recipient.ToRaw(), jettons)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[ton.AccountID]core.AccountInfo)
	for rows.Next() {
		var (
			acc string
			jet string
		)
		info := core.AccountInfo{
			Recipient: recipient,
		}
		err = rows.Scan(&acc, &info.MaxDepthLt, &jet)
		if err != nil {
			return nil, err
		}
		account, err := ton.ParseAccountID(acc)
		if err != nil {
			return nil, err
		}
		jetton, err := ton.ParseAccountID(jet)
		if err != nil {
			return nil, err
		}
		info.Jetton = &jetton
		res[account] = info
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	startLT, err := c.GetAccountStartLT(ctx, recipient)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		return res, nil
	} else if err != nil {
		return nil, err
	}
	res[recipient] = core.AccountInfo{
		Recipient:  recipient,
		MaxDepthLt: startLT,
	}
	return res, nil
}

func (c *Connection) UpdateAccount(ctx context.Context, account ton.AccountID, lastTX core.TxID, mcSeqno uint32) error {
	_, err := c.postgres.Exec(ctx, `
		UPDATE blockchain.accounts 
		SET last_tx_lt = $1, last_tx_hash = $2, last_checked_block = $3, indexer_timestamp = $4 
		WHERE address = $5`,
		lastTX.Lt, lastTX.Hash, mcSeqno, time.Now(), account.ToRaw())
	return err
}

func (c *Connection) CreateAccount(ctx context.Context, account core.Account, lastTxID core.TxID) error {
	if account.Info.Jetton != nil {
		return c.createJettonAccount(ctx, account, lastTxID)
	}
	// set MaxDepthLt as last_processed_lt to avoid indexing the account history from first transaction
	_, err := c.postgres.Exec(ctx, `
		INSERT INTO blockchain.accounts (address, last_tx_lt, last_tx_hash, indexer_timestamp, start_tx_lt, last_processed_lt)
		VALUES ($1, $2, $3, $4, $5, $6)`, account.AccountID.ToRaw(), lastTxID.Lt, lastTxID.Hash, time.Now(), account.Info.MaxDepthLt, account.Info.MaxDepthLt)
	return err
}

func (c *Connection) GetAccountStartLT(ctx context.Context, account ton.AccountID) (uint64, error) {
	var startLT uint64
	err := c.postgres.QueryRow(ctx, `
		SELECT start_tx_lt
		FROM blockchain.accounts WHERE address = $1`, account.ToRaw()).Scan(&startLT)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return 0, core.ErrNotFound
	} else if err != nil {
		return 0, err
	}
	return startLT, nil
}

func (c *Connection) createJettonAccount(ctx context.Context, account core.Account, lastTxID core.TxID) error {
	if account.Info.Jetton == nil {
		return errors.New("not jetton account")
	}
	currencyID, err := c.getCurrencyID(ctx, core.JettonCurrency(*account.Info.Jetton))
	if err != nil {
		return err
	}

	tx, err := c.postgres.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackDbTx(ctx, tx)

	_, err = tx.Exec(ctx, `
		INSERT INTO blockchain.accounts (address, last_tx_lt, last_tx_hash, indexer_timestamp, start_tx_lt, last_processed_lt) 
		VALUES ($1, $2, $3, $4, $5, $6)`, account.AccountID.ToRaw(), lastTxID.Lt, lastTxID.Hash, time.Now(), account.Info.MaxDepthLt, account.Info.MaxDepthLt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO payments.jetton_wallets (address, owner, currency) 
		VALUES ($1, $2, $3)`, account.AccountID.ToRaw(), account.Info.Recipient.ToRaw(), currencyID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c *Connection) LastProcessedLT(ctx context.Context, a ton.AccountID) (uint64, error) {
	var (
		lastProcessedLt uint64
	)
	err := c.postgres.QueryRow(ctx, `
		SELECT last_processed_lt FROM blockchain.accounts WHERE address = $1`, a).Scan(&lastProcessedLt)
	if err != nil {
		return 0, err
	}
	return lastProcessedLt, nil
}
