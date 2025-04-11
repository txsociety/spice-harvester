package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
)

func (c *Connection) GetTransactionByParentLt(ctx context.Context, a ton.AccountID, lt uint64) (core.Transaction, error) {
	var tx core.Transaction
	var inMessageBytes []byte
	var outMessages [][]byte
	err := c.postgres.QueryRow(ctx, ` 
		SELECT hash, lt, prev_tx_hash, prev_tx_lt, utime, in_message, out_messages, success 
 		FROM blockchain.transactions WHERE account_id = $1 AND prev_tx_lt = $2`, a, lt).
		Scan(&tx.Hash, &tx.Lt, &tx.PrevTxHash, &tx.PrevTxLt, &tx.Utime, &inMessageBytes, &outMessages, &tx.Success)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.Transaction{}, core.ErrNotFound
	}
	if err != nil {
		return core.Transaction{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(inMessageBytes))
	decoder.UseNumber()
	err = decoder.Decode(&tx.InMessage)
	if err != nil {
		return core.Transaction{}, err
	}
	for _, m := range outMessages {
		var msg core.Message
		decoder := json.NewDecoder(bytes.NewReader(m))
		decoder.UseNumber()
		err = decoder.Decode(&msg)
		if err != nil {
			return core.Transaction{}, err
		}
		tx.OutMessages = append(tx.OutMessages, msg)
	}
	return tx, nil
}

func (c *Connection) SaveTransactions(ctx context.Context, a ton.AccountID, txs []core.Transaction) error {
	for _, tx := range txs {
		inMessageBytes, err := marshalJsonForDb(tx.InMessage)
		if err != nil {
			slog.Error("failed to marshal msg for transaction", "error", err.Error(), "tx_hash", tx.Hash.Hex())
		}
		outMessages := make([][]byte, len(tx.OutMessages))
		for i, m := range tx.OutMessages {
			msgBytes, err := marshalJsonForDb(m)
			if err != nil {
				slog.Error("failed to marshal msg for transaction", "error", err.Error(), "tx_hash", tx.Hash.Hex())
			}
			outMessages[i] = msgBytes
		}
		_, err = c.postgres.Exec(ctx, `
			INSERT INTO blockchain.transactions
			(hash, lt, account_id, prev_tx_hash, prev_tx_lt, utime, in_message, out_messages, success) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
			)  ON CONFLICT (hash) DO NOTHING
		`, tx.Hash, tx.Lt, a.String(), tx.PrevTxHash, tx.PrevTxLt, tx.Utime, inMessageBytes, outMessages, tx.Success)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetGaps returns gaps in transaction history for account and last transaction in db
func (c *Connection) GetGaps(ctx context.Context, a ton.AccountID) ([]core.TxGap, uint64, error) {
	rows, err := c.postgres.Query(ctx, `
		SELECT tx.prev_tx_lt, tx.prev_tx_hash FROM blockchain.transactions tx 
        LEFT JOIN blockchain.transactions ptx ON tx.prev_tx_hash = ptx.hash
        WHERE ptx.hash IS NULL AND tx.account_id = $1 AND tx.prev_tx_lt != 0`, a)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var gaps []core.TxGap
	for rows.Next() {
		var gap core.TxGap
		err := rows.Scan(&gap.StartLt, &gap.StartHash)
		if err != nil {
			return nil, 0, err
		}
		gaps = append(gaps, gap)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	for i, gap := range gaps {
		err := c.postgres.QueryRow(ctx, `
			SELECT lt FROM blockchain.transactions WHERE account_id = $1 AND lt < $2 ORDER BY lt DESC LIMIT 1`, a, gap.StartLt).Scan(&gap.EndLt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, err
		}
		gaps[i] = gap
	}
	var lastLt uint64
	err = c.postgres.QueryRow(ctx, `
		SELECT lt FROM blockchain.transactions WHERE account_id = $1 ORDER BY lt DESC LIMIT 1`, a).Scan(&lastLt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, err
	}
	return gaps, lastLt, nil
}
