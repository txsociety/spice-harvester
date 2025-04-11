package indexer

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/tonkeeper/tongo/abi"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"math/big"
	"time"
)

type indexerWorker struct {
	account     core.Account
	storage     storage
	lastIndexed uint64
}

func newIndexerWorker(storage storage, a core.Account) (*indexerWorker, error) {
	t := &indexerWorker{
		storage: storage,
		account: a,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lastIndexed, err := t.storage.LastProcessedLT(ctx, t.account.AccountID)
	if err != nil {
		return nil, err
	}
	t.lastIndexed = lastIndexed
	return t, nil
}

func (i *indexerWorker) Run() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		tx, err := i.storage.GetTransactionByParentLt(ctx, i.account.AccountID, i.lastIndexed)
		if err != nil {
			if !errors.Is(err, core.ErrNotFound) {
				slog.Error("error getting tx", " error", err)
			}
			time.Sleep(5 * time.Second)
			cancel()
			continue
		}
		var payments []core.Payment
		if i.account.Info.Jetton != nil {
			payments, err = extractJettonPayments(tx, i.account)
		} else {
			payments, err = extractNativePayments(tx, i.account)
		}
		err = i.storage.SavePayments(ctx, i.account.AccountID, tx.Lt, payments, err)
		if err != nil {
			slog.Error("saving tx", "error", err)
			time.Sleep(5 * time.Second)
			cancel()
			continue
		}
		i.lastIndexed = tx.Lt
		cancel()
	}
}

func extractNativePayments(tx core.Transaction, account core.Account) ([]core.Payment, error) {
	if !tx.Success {
		return nil, nil
	}
	if tx.InMessage.Type != "Int" { // not internal message
		return nil, nil
	}
	if tx.InMessage.DecodedOperation != abi.InvoicePayloadMsgOp {
		return nil, nil
	}

	var (
		idBytes []byte
		err     error
	)
	if body, ok := tx.InMessage.DecodedBody.(map[string]any); ok {
		idS, err := valueFromBody[string](body, "Id")
		if err != nil {
			return nil, err
		}
		idBytes, err = hex.DecodeString(idS)
		if err != nil {
			return nil, err
		}
	}

	var res []core.Payment
	tons := tx.InMessage.Value
	id, err := core.InvoiceIdFromBytes(idBytes)
	if err != nil {
		return nil, nil
	}
	res = append(res, core.Payment{
		InvoiceID: id,
		PaidBy:    *tx.InMessage.Source,
		Amount:    big.NewInt(int64(tons)),
		TxHash:    tx.Hash,
		Currency:  core.TonCurrency(),
		Recipient: account.AccountID,
	})
	for extraID, value := range tx.InMessage.ExtraCurrencies {
		amount := big.Int(value)
		if amount.Cmp(big.NewInt(0)) == 0 {
			continue
		}
		res = append(res, core.Payment{
			InvoiceID: id,
			Recipient: account.AccountID,
			PaidBy:    *tx.InMessage.Source,
			Amount:    &amount,
			TxHash:    tx.Hash,
			Currency:  core.ExtraCurrency(extraID),
		})
	}
	return res, nil
}

func extractJettonPayments(tx core.Transaction, account core.Account) ([]core.Payment, error) {
	if !tx.Success {
		return nil, nil
	}
	if tx.InMessage.Type != "Int" { // not internal message
		return nil, nil
	}

	for _, outMsg := range tx.OutMessages {
		if outMsg.Type != "Int" {
			return nil, nil
		}
		// skip bounced check. bounced must decode as bounced

		if outMsg.DecodedOperation != abi.JettonNotifyMsgOp {
			continue
		}

		body, ok := outMsg.DecodedBody.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid decoded body")
		}
		amountS, err := valueFromBody[string](body, "Amount")
		if err != nil {
			return nil, fmt.Errorf("invalid amount: %w", err)
		}
		amount, ok := new(big.Int).SetString(amountS, 10)
		if !ok {
			return nil, fmt.Errorf("invalid amount")
		}
		senderS, err := valueFromBody[string](body, "Sender")
		if err != nil {
			return nil, fmt.Errorf("invalid sender: %w", err)
		}
		sender, err := ton.ParseAccountID(senderS)
		if err != nil {
			return nil, fmt.Errorf("invalid sender: %w", err)
		}

		var idBytes []byte
		if forwardPayload, err := valueFromBody[map[string]any](body, "ForwardPayload"); err == nil {
			if value, err := valueFromBody[map[string]any](forwardPayload, "Value"); err == nil {
				if sumType, _ := valueFromBody[string](value, "SumType"); sumType == "InvoicePayload" {
					if payload, err := valueFromBody[map[string]any](value, "Value"); err == nil {
						idS, err := valueFromBody[string](payload, "Id")
						if err != nil {
							return nil, err
						}
						idBytes, err = hex.DecodeString(idS)
						if err != nil {
							return nil, err
						}
					}
				}
			}
		}

		if outMsg.Destination == nil {
			return nil, fmt.Errorf("empty destination from jetton notify")
		}
		if *outMsg.Destination != account.Info.Recipient {
			return nil, fmt.Errorf("invalid destination from jetton notify")
		}

		id, err := core.InvoiceIdFromBytes(idBytes)
		if err != nil {
			return nil, nil
		}
		return []core.Payment{
			{
				InvoiceID: id,
				Amount:    amount,
				Currency:  core.JettonCurrency(*account.Info.Jetton),
				PaidBy:    sender,
				Recipient: account.Info.Recipient,
				TxHash:    tx.Hash,
			},
		}, nil
	}
	return nil, nil
}

func valueFromBody[T any](body map[string]any, key string) (T, error) {
	var t T
	v, ok := body[key]
	if !ok {
		return t, fmt.Errorf("no %v found", key)
	}
	s, ok := v.(T)
	if !ok {
		return t, fmt.Errorf("invalid type %v", key)
	}
	return s, nil
}
