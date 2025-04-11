package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"time"
)

func (c *Connection) SaveEncryptionKey(ctx context.Context, account ton.AccountID, encryptionKey []byte) error {
	_, err := c.postgres.Exec(ctx, `
		INSERT INTO payments.keys (address, encryption_key, created_at) 
		VALUES ($1, $2, $3) 		
		ON CONFLICT DO NOTHING`,
		account.ToRaw(), encryptionKey, time.Now())
	return err
}

func (c *Connection) GetEncryptionKey(ctx context.Context, account ton.AccountID) ([]byte, error) {
	var key []byte
	err := c.postgres.QueryRow(ctx, `
		SELECT encryption_key 
		FROM payments.keys 
		WHERE address = $1 and accepted = true`, account.ToRaw()).Scan(&key)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return nil, core.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return key, nil
}

func (c *Connection) DeleteExpiredKeys(ctx context.Context) error {
	_, err := c.postgres.Exec(ctx, `
		DELETE  
		FROM payments.keys 
		WHERE created_at < $1 and accepted = false`, time.Now().Add(-time.Hour))
	return err
}
