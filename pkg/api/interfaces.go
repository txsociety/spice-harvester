package api

import (
	"context"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
)

type storage interface {
	CreateInvoice(ctx context.Context, newInvoice core.Invoice) error
	GetInvoice(ctx context.Context, id core.InvoiceID) (core.Invoice, error)
	CancelInvoice(ctx context.Context, id core.InvoiceID) (core.Invoice, error)
	SaveEncryptionKey(ctx context.Context, account ton.AccountID, encryptionKey []byte) error
	GetEncryptionKey(ctx context.Context, account ton.AccountID) ([]byte, error)
	GetInvoices(ctx context.Context, after core.InvoiceID, limit int64) ([]core.Invoice, error)
	GetRecipient(ctx context.Context) (ton.AccountID, error)
}
