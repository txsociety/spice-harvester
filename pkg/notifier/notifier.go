package notifier

import (
	"context"
	"fmt"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"sync"
	"time"
)

type Notifier struct {
	sender          sender
	currencies      map[string]core.ExtendedCurrency
	adnlAddress     *ton.Bits256
	paymentPrefixes map[string]string
	storage         storage
}

func New(sender sender, currencies map[string]core.ExtendedCurrency, adnlAddress *ton.Bits256, paymentPrefixes map[string]string, storage storage) *Notifier {
	return &Notifier{
		sender:          sender,
		currencies:      currencies,
		adnlAddress:     adnlAddress,
		paymentPrefixes: paymentPrefixes,
		storage:         storage,
	}
}

func (n *Notifier) Run(ctx context.Context, wg *sync.WaitGroup) {
	go n.runNotifyExpirationProcessor(ctx, wg)
	if n.sender != nil {
		go n.runNotifier(ctx, wg)
	}
}

func (n *Notifier) runNotifier(ctx context.Context, wg *sync.WaitGroup) {
	slog.Info("notifier started")
	wg.Add(1)
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			slog.Info("notifier stopped")
			return
		default:
			limit := 10
			invoices, err := n.storage.GetInvoiceNotifications(ctx, limit)
			if err != nil {
				slog.Error("get notifications", "error", err.Error())
				time.Sleep(3 * time.Second)
				continue
			}
			err = n.notify(ctx, invoices)
			if err != nil {
				slog.Error("notify failed", "error", err.Error())
				time.Sleep(3 * time.Second)
				continue
			}
			if len(invoices) < limit {
				time.Sleep(2 * time.Second)
			}
		}
	}
}

func (n *Notifier) notify(ctx context.Context, invoices []core.Invoice) error {
	for _, invoice := range invoices {
		invoiceP, err := core.ConvertInvoiceToPrintablePrivate(n.paymentPrefixes, invoice, n.currencies, n.adnlAddress)
		if err != nil {
			slog.Error("convert invoice to printable", "error", err.Error())
			continue // can not send this invoice
		}
		err = n.sender.Send(ctx, invoiceP)
		if err != nil {
			return fmt.Errorf("send invoice to sender err: %w", err)
		}
		err = n.storage.DeleteInvoiceNotification(ctx, invoice.ID)
		if err != nil {
			return fmt.Errorf("delete notification err: %w", err)
		}
	}
	return nil
}

func (n *Notifier) runNotifyExpirationProcessor(ctx context.Context, wg *sync.WaitGroup) {
	slog.Info("notify expiration processor started")
	wg.Add(1)
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			slog.Info("notify expiration processor stopped")
			return
		case <-time.After(30 * time.Second):
			err := n.storage.DeleteOldNotifications(ctx)
			if err != nil {
				slog.Error("delete old notifications", "error", err.Error())
			}
		}
	}
}
