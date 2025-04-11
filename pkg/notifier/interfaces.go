package notifier

import (
	"context"
	"github.com/txsociety/spice-harvester/pkg/core"
)

type sender interface {
	Send(ctx context.Context, invoice core.InvoicePrintable) error
}

type storage interface {
	GetInvoiceNotifications(ctx context.Context, limit int) ([]core.Invoice, error)
	DeleteInvoiceNotification(ctx context.Context, invoiceID core.InvoiceID) error
	DeleteOldNotifications(ctx context.Context) error
}
