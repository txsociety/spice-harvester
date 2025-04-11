package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"math/big"
	"time"
)

func (c *Connection) CreateInvoice(ctx context.Context, invoice core.Invoice) error {
	err := c.saveInvoice(ctx, invoice, false)
	if err != nil {
		return err
	}
	return c.saveInvoice(ctx, invoice, true)
}

func (c *Connection) saveInvoice(ctx context.Context, invoice core.Invoice, isNotify bool) error {
	currencyID, err := c.getCurrencyID(ctx, invoice.Currency)
	if err != nil {
		return err
	}
	metaBytes, err := marshalJsonForDb(invoice.Metadata)
	if err != nil {
		return err
	}
	privateInfoBytes, err := marshalJsonForDb(invoice.PrivateInfo)
	if err != nil {
		return err
	}
	table := "payments.invoices"
	if isNotify {
		table = "payments.invoice_notifications"
	}
	sqlRequest := fmt.Sprintf(`INSERT INTO %s 
		(id, status, amount, currency, created_at, expire_at, updated_at, private_info, metadata, overpayment, recipient)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, table)
	_, err = c.postgres.Exec(ctx, sqlRequest,
		invoice.ID,
		invoice.Status,
		invoice.Amount.String(),
		currencyID,
		invoice.CreatedAt,
		invoice.ExpireAt,
		invoice.UpdatedAt,
		privateInfoBytes,
		metaBytes,
		invoice.Overpayment.String(),
		invoice.Recipient,
	)
	if err != nil {
		return err
	}
	return nil
}

func (c *Connection) GetInvoice(ctx context.Context, id core.InvoiceID) (core.Invoice, error) {
	var (
		i                              core.Invoice
		currencyID                     uuid.UUID
		recipient, amount, overpayment string
		paidByS                        *string
		txHash                         *ton.Bits256
	)
	err := c.postgres.QueryRow(ctx, `
		SELECT id, status, amount, currency, created_at, expire_at, updated_at, private_info, metadata, overpayment, paid_at, paid_by, recipient, tx_hash
		FROM payments.invoices WHERE id = $1`, id).Scan(
		&i.ID,
		&i.Status,
		&amount,
		&currencyID,
		&i.CreatedAt,
		&i.ExpireAt,
		&i.UpdatedAt,
		&i.PrivateInfo,
		&i.Metadata,
		&overpayment,
		&i.PaidAt,
		&paidByS,
		&recipient,
		&txHash,
	)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return core.Invoice{}, core.ErrNotFound
	} else if err != nil {
		return core.Invoice{}, err
	}
	currency, err := c.getCurrencyByID(ctx, currencyID)
	if err != nil {
		return core.Invoice{}, err
	}
	i.Currency = *currency
	i.Recipient, err = ton.ParseAccountID(recipient)
	if err != nil {
		return core.Invoice{}, err
	}
	i.Amount, _ = new(big.Int).SetString(amount, 10)
	i.Overpayment, _ = new(big.Int).SetString(overpayment, 10)
	if paidByS != nil {
		paidBy, err := ton.ParseAccountID(*paidByS)
		if err != nil {
			return core.Invoice{}, err
		}
		i.PaidBy = &paidBy
	}
	if txHash != nil {
		i.TxHash = txHash
	}
	return i, nil
}

func (c *Connection) GetInvoices(ctx context.Context, after core.InvoiceID, limit int64) ([]core.Invoice, error) {
	// use validated id
	rows, err := c.postgres.Query(ctx, `
		SELECT id
		FROM payments.invoices		
		WHERE id > $1
		ORDER BY id
		LIMIT $2`,
		after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invoiceIDs []core.InvoiceID

	for rows.Next() {
		var invoiceID core.InvoiceID
		err = rows.Scan(&invoiceID)
		if err != nil {
			return nil, err
		}
		invoiceIDs = append(invoiceIDs, invoiceID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return c.getInvoicesByIDs(ctx, invoiceIDs)
}

func (c *Connection) getInvoicesByIDs(ctx context.Context, invoiceIDs []core.InvoiceID) (res []core.Invoice, err error) {
	for _, id := range invoiceIDs {
		inv, err := c.GetInvoice(ctx, id)
		if err != nil {
			return nil, err
		}
		res = append(res, inv)
	}
	return res, nil
}

func (c *Connection) GetInvoiceNotifications(ctx context.Context, limit int) ([]core.Invoice, error) {
	rows, err := c.postgres.Query(ctx, `
		SELECT id
		FROM payments.invoice_notifications
		ORDER BY updated_at
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invoiceIDs []core.InvoiceID

	for rows.Next() {
		var invoiceID core.InvoiceID
		err = rows.Scan(&invoiceID)
		if err != nil {
			return nil, err
		}
		invoiceIDs = append(invoiceIDs, invoiceID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return c.getInvoicesByIDs(ctx, invoiceIDs)
}

func (c *Connection) DeleteInvoiceNotification(ctx context.Context, invoiceID core.InvoiceID) error {
	_, err := c.postgres.Exec(ctx, `
		DELETE FROM payments.invoice_notifications
		WHERE id = $1`, invoiceID)
	return err
}

func (c *Connection) DeleteOldNotifications(ctx context.Context) error {
	_, err := c.postgres.Exec(ctx, `
		DELETE FROM payments.invoice_notifications
		WHERE updated_at < $1`, time.Now().Add(-time.Hour*24*5))
	return err
}

func (c *Connection) CancelInvoice(ctx context.Context, id core.InvoiceID) (core.Invoice, error) {
	now := time.Now()
	tag, err := c.postgres.Exec(ctx, `
		UPDATE payments.invoices 
		SET status = $1, updated_at = $2 
		WHERE id = $3 AND status = $4 AND expire_at > $5`,
		core.CanceledInvoiceStatus, now, id, core.WaitingInvoiceStatus, now)
	if err != nil {
		return core.Invoice{}, err
	}
	if tag.RowsAffected() == 0 {
		return core.Invoice{}, core.ErrNotFound
	}
	invoice, err := c.GetInvoice(ctx, id)
	if err != nil {
		return core.Invoice{}, err
	}
	err = c.saveInvoice(ctx, invoice, true)
	if err != nil {
		return core.Invoice{}, err
	}
	return invoice, nil
}

func (c *Connection) MarkExpired(ctx context.Context) error {
	now := time.Now()
	rows, err := c.postgres.Query(ctx, `
		UPDATE payments.invoices 
		SET status = $1, updated_at = $2 
		WHERE status = $3 AND expire_at < $4
		RETURNING id`,
		core.ExpiredInvoiceStatus, now, core.WaitingInvoiceStatus, now)
	if err != nil {
		return err
	}
	defer rows.Close()

	var invoiceIDs []core.InvoiceID

	for rows.Next() {
		var invoiceID core.InvoiceID
		err = rows.Scan(&invoiceID)
		if err != nil {
			return err
		}
		invoiceIDs = append(invoiceIDs, invoiceID)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range invoiceIDs {
		inv, err := c.GetInvoice(ctx, id)
		if err != nil {
			return err
		}
		err = c.saveInvoice(ctx, inv, true)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Connection) SavePayments(ctx context.Context, account ton.AccountID, txLt uint64, payments []core.Payment, parsingError error) error {
	tx, err := c.postgres.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackDbTx(ctx, tx)

	var res []core.Invoice
	if parsingError != nil {
		_, err = tx.Exec(ctx, `
			UPDATE blockchain.transactions set processing_error = $1 where account_id = $2 and lt = $3`,
			parsingError.Error(), account.ToRaw(), txLt)
		if err != nil {
			return err
		}
	} else if len(payments) > 0 {
		for _, p := range payments {
			inv, err := c.processPayment(ctx, tx, p)
			if err != nil {
				return err
			}
			res = append(res, inv...)
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE blockchain.accounts set last_processed_lt = $1 where address = $2`, txLt, account.ToRaw())
	if err != nil {
		return err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return err
	}
	for _, inv := range res {
		err = c.saveInvoice(ctx, inv, true)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Connection) processPayment(ctx context.Context, tx pgx.Tx, p core.Payment) ([]core.Invoice, error) {
	currencyID, err := c.getCurrencyID(ctx, p.Currency)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		return nil, nil // not tracked currency
	} else if err != nil {
		return nil, err
	}

	var (
		status, amountS, overpaymentS string
		metadata, privateInfo         map[string]json.RawMessage
		expireAt, createdAt           time.Time
	)

	now := time.Now()
	err = tx.QueryRow(ctx, `
		SELECT expire_at, created_at, amount, status, metadata, private_info, overpayment
		FROM payments.invoices
		WHERE currency = $1 AND id = $2 AND recipient = $3`, *currencyID, p.InvoiceID, p.Recipient.ToRaw()).Scan(
		&expireAt, &createdAt, &amountS, &status, &metadata, &privateInfo, &overpaymentS)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	amount, _ := new(big.Int).SetString(amountS, 10)
	overpayment, _ := new(big.Int).SetString(overpaymentS, 10)

	_, err = tx.Exec(ctx, `
		UPDATE payments.keys
		SET accepted = true
		WHERE address = $1 AND accepted = false`, p.PaidBy.ToRaw())
	if err != nil {
		return nil, err
	}

	overpayment.Add(overpayment, p.Amount)
	_, err = tx.Exec(ctx, `
		UPDATE payments.invoices
		SET overpayment = $1, updated_at = $2
		WHERE id = $3`, overpayment, now, p.InvoiceID)
	if err != nil {
		return nil, err
	}
	if status != string(core.WaitingInvoiceStatus) || expireAt.Before(now) {
		return nil, nil
	}
	if overpayment.Cmp(amount) == -1 { // overpayment < amount
		return nil, nil
	}
	overpayment.Sub(overpayment, amount)

	_, err = tx.Exec(ctx, `
			UPDATE payments.invoices
			SET status = $1, updated_at = $2, paid_by = $3, overpayment = $4, paid_at = $5, tx_hash = $6
			WHERE id = $7`, core.PaidInvoiceStatus, now, p.PaidBy.ToRaw(), overpayment, now, p.TxHash, p.InvoiceID)
	if err != nil {
		return nil, err
	}
	res := core.Invoice{
		ID:          p.InvoiceID,
		Recipient:   p.Recipient,
		Status:      core.PaidInvoiceStatus,
		Amount:      amount,
		Currency:    p.Currency,
		CreatedAt:   createdAt,
		ExpireAt:    expireAt,
		UpdatedAt:   now,
		PrivateInfo: privateInfo,
		Metadata:    metadata,
		PaidBy:      &p.PaidBy,
		PaidAt:      &now,
		Overpayment: overpayment,
	}
	return []core.Invoice{res}, nil
}

func rollbackDbTx(ctx context.Context, tx pgx.Tx) {
	err := tx.Rollback(ctx)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.Error("rolling back db transaction", "error", err.Error())
	}
}

func marshalJsonForDb(x any) ([]byte, error) {
	b, err := json.Marshal(x)
	if err != nil {
		return nil, err
	}
	b = bytes.ReplaceAll(b, []byte(`\u0000`), nil) // postgres doesn't support \u0000 in jsonb
	return b, nil
}

func (c *Connection) GetRecipient(ctx context.Context) (ton.AccountID, error) {
	return c.recipient, nil
}
