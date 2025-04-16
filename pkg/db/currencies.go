package db

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"strconv"
)

func (c *Connection) getCurrencyID(ctx context.Context, cur core.Currency) (*uuid.UUID, error) {
	var (
		id  uuid.UUID
		err error
	)
	switch cur.Type {
	case core.TON:
		err = c.postgres.QueryRow(ctx, `
		SELECT id
		FROM payments.currencies WHERE type = $1`, cur.Type).Scan(&id)
	case core.Extra:
		err = c.postgres.QueryRow(ctx, `
		SELECT id
		FROM payments.currencies WHERE type = $1 and info = $2`, cur.Type, fmt.Sprintf("%d", *cur.ExtraID())).Scan(&id)
	case core.Jetton:
		err = c.postgres.QueryRow(ctx, `
		SELECT id
		FROM payments.currencies WHERE type = $1 and info = $2`, cur.Type, cur.Jetton().ToRaw()).Scan(&id)
	}
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return nil, core.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &id, nil
}

// TODO: or use in memory map
func (c *Connection) getCurrencyByID(ctx context.Context, currencyID uuid.UUID) (*core.Currency, error) {
	var (
		curType core.CurrencyType
		info    string
	)
	err := c.postgres.QueryRow(ctx, `
		SELECT type, info
		FROM payments.currencies WHERE id = $1`, currencyID).Scan(&curType, &info)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return nil, core.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var res core.Currency
	switch curType {
	case core.TON:
		res = core.TonCurrency()
	case core.Extra:
		id, err := strconv.ParseInt(info, 10, 32)
		if err != nil {
			return nil, err
		}
		res = core.ExtraCurrency(uint32(id))
	case core.Jetton:
		addr, err := ton.ParseAccountID(info)
		if err != nil {
			return nil, err
		}
		res = core.JettonCurrency(addr)
	}
	return &res, nil
}

func (c *Connection) SaveCurrencies(ctx context.Context, currencies map[string]core.ExtendedCurrency) error {
	for _, currency := range currencies {
		err := c.saveCurrency(ctx, currency.Currency)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Connection) saveCurrency(ctx context.Context, currency core.Currency) error {
	var info string
	switch currency.Type {
	case core.Jetton:
		info = currency.Jetton().ToRaw()
	case core.Extra:
		info = fmt.Sprintf("%d", int64(*currency.ExtraID()))
	}
	_, err := c.postgres.Exec(ctx, `
		INSERT INTO payments.currencies (type, info) 
		VALUES ($1, $2) 		
		ON CONFLICT DO NOTHING`,
		currency.Type, info)
	return err
}
