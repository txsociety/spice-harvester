package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/tonkeeper/tongo/ton"
)

func (c *Connection) GetLastTrustedBlock(ctx context.Context) (*ton.BlockIDExt, error) {
	blockID := ton.BlockIDExt{
		BlockID: ton.BlockID{
			Workchain: -1,
			Shard:     0x8000000000000000,
		},
	}
	err := c.postgres.QueryRow(ctx, `
		SELECT seqno, root_hash, file_hash 
		FROM blockchain.trusted_mc_block 
		WHERE id = 1`).Scan(&blockID.Seqno, &blockID.RootHash, &blockID.FileHash)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &blockID, nil
}

func (c *Connection) SetLastTrustedBlock(ctx context.Context, block ton.BlockIDExt) error {
	if block.Workchain != -1 || block.Shard != 0x8000000000000000 {
		return errors.New("only masterchain block can be saved")
	}
	_, err := c.postgres.Exec(ctx, `
		INSERT INTO blockchain.trusted_mc_block
   		(id, seqno, root_hash, file_hash)
		VALUES (1, $1, $2, $3)
		ON CONFLICT (id) DO UPDATE
   		SET seqno = $1, root_hash = $2, file_hash = $3`,
		block.Seqno, block.RootHash, block.FileHash)
	return err
}
