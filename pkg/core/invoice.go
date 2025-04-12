package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/tonkeeper/tongo/ton"
	"math/big"
	"time"
)

type InvoiceStatus string

const (
	WaitingInvoiceStatus  InvoiceStatus = "waiting"
	PaidInvoiceStatus     InvoiceStatus = "paid"
	CanceledInvoiceStatus InvoiceStatus = "cancelled"
	ExpiredInvoiceStatus  InvoiceStatus = "expired"
)

type Invoice struct {
	ID          InvoiceID
	Recipient   ton.AccountID
	Status      InvoiceStatus
	Amount      *big.Int
	Overpayment *big.Int
	Currency    Currency
	CreatedAt   time.Time
	ExpireAt    time.Time
	UpdatedAt   time.Time
	PrivateInfo map[string]json.RawMessage
	Metadata    map[string]json.RawMessage
	PaidBy      *ton.AccountID
	PaidAt      *time.Time
	TxHash      *ton.Bits256
}

type PrivateInvoicePrintable struct {
	PublicInvoicePrintable
	PrivateInfo map[string]json.RawMessage `json:"private_info"`
	Metadata    map[string]json.RawMessage `json:"metadata"`
}

type PublicInvoicePrintable struct {
	ID           string            `json:"id"`
	Status       string            `json:"status"`
	Amount       string            `json:"amount"`
	Currency     string            `json:"currency"`
	Recipient    string            `json:"pay_to_address"`
	PaymentLinks map[string]string `json:"payment_links"`
	CreatedAt    int64             `json:"created_at"`
	ExpireAt     int64             `json:"expire_at"`
	UpdatedAt    int64             `json:"updated_at"`
	Overpayment  string            `json:"overpayment"`
	PaidBy       string            `json:"paid_by,omitempty"`
	PaidAt       *int64            `json:"paid_at,omitempty"`
	TxHash       string            `json:"tx_hash,omitempty"`
}

func ConvertInvoiceToPrintablePublic(prefixes map[string]string, invoice Invoice, currencies map[string]Currency, adnlAddress *ton.Bits256) (PublicInvoicePrintable, error) {
	ticker := ""
	for t, c := range currencies {
		if c == invoice.Currency {
			ticker = t
		}
	}
	if len(ticker) == 0 {
		return PublicInvoicePrintable{}, fmt.Errorf("currency not found: %s", invoice.Currency.String())
	}
	res := PublicInvoicePrintable{
		ID:           invoice.ID.String(),
		Status:       string(invoice.Status),
		Amount:       invoice.Amount.String(),
		Currency:     ticker,
		Recipient:    invoice.Recipient.ToRaw(),
		CreatedAt:    invoice.CreatedAt.Unix(),
		ExpireAt:     invoice.ExpireAt.Unix(),
		UpdatedAt:    invoice.UpdatedAt.Unix(),
		Overpayment:  invoice.Overpayment.String(),
		PaymentLinks: make(map[string]string, len(prefixes)),
	}
	for name, prefix := range prefixes {
		paymentLink, err := GeneratePaymentLink(prefix, invoice, adnlAddress)
		if err != nil {
			return PublicInvoicePrintable{}, err
		}
		res.PaymentLinks[name] = paymentLink
	}
	if invoice.PaidAt != nil {
		paidAt := invoice.PaidAt.Unix()
		res.PaidAt = &paidAt
	}
	if invoice.PaidBy != nil {
		res.PaidBy = invoice.PaidBy.ToRaw()
	}
	if invoice.TxHash != nil {
		res.TxHash = invoice.TxHash.Hex()
	}
	return res, nil
}

func ConvertInvoiceToPrintablePrivate(prefixes map[string]string, invoice Invoice, currencies map[string]Currency, adnlAddress *ton.Bits256) (PrivateInvoicePrintable, error) {
	publicInvoice, err := ConvertInvoiceToPrintablePublic(prefixes, invoice, currencies, adnlAddress)
	if err != nil {
		return PrivateInvoicePrintable{}, err
	}
	return PrivateInvoicePrintable{
		PublicInvoicePrintable: publicInvoice,
		PrivateInfo:            invoice.PrivateInfo,
		Metadata:               invoice.Metadata,
	}, nil
}

type Payment struct {
	InvoiceID InvoiceID
	Currency  Currency
	Amount    *big.Int
	PaidBy    ton.AccountID
	Recipient ton.AccountID
	TxHash    ton.Bits256
}

type InvoiceID = uuid.UUID

func ParseInvoiceID(id string) (InvoiceID, error) {
	if len(id) == 0 {
		return InvoiceID{}, errors.New("invalid id length")
	}
	res, err := uuid.Parse(id)
	if err != nil {
		return InvoiceID{}, err
	}
	if res.Version() != 7 {
		return InvoiceID{}, fmt.Errorf("invalid invoice id")
	}
	return res, nil
}

func NewInvoiceID() InvoiceID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}

func InvoiceIdFromBytes(data []byte) (InvoiceID, error) {
	res, err := uuid.FromBytes(data)
	if err != nil {
		return InvoiceID{}, err
	}
	if res.Version() != 7 {
		return InvoiceID{}, fmt.Errorf("invalid invoice id")
	}
	return res, nil
}

var DefaultPaymentPrefixes = map[string]string{
	"universal": "ton://",
	"tonkeeper": "https://app.tonkeeper.com/",
}

func GeneratePaymentLink(prefix string, invoice Invoice, adnlAddress *ton.Bits256) (string, error) {
	payload, err := EncodePayload(invoice, adnlAddress)
	if err != nil {
		return "", err
	}
	switch invoice.Currency.Type {
	case TON:
		// {prefix}transfer/{address}?amount={elementary-units}&bin={base64url-binary-data}&exp={expiry-timestamp}
		link := fmt.Sprintf("%stransfer/%s?amount=%d&bin=%s&exp=%d",
			prefix, invoice.Recipient.ToHuman(false, false), invoice.Amount, payload, invoice.ExpireAt.Unix())
		return link, nil
	case Jetton:
		// {prefix}transfer/{destination-address}?jetton={jetton-master-address}&amount={elementary-units}&bin={base64url-binary-data}&exp={expiry-timestamp}
		link := fmt.Sprintf("%stransfer/%s?jetton=%s&amount=%d&bin=%s&exp=%d",
			prefix, invoice.Recipient.ToHuman(true, false), invoice.Currency.Jetton().ToHuman(true, false),
			invoice.Amount, payload, invoice.ExpireAt.Unix())
		return link, nil
	case Extra:
		// TODO: implement
		return "", errors.New("extra not supported yet")
	}
	return "", errors.New("unknown currency type")
}

type InvoiceMetadata struct {
	MerchantName string        `json:"merchant_name"`
	MerchantURL  string        `json:"merchant_url,omitempty"`
	MerchantLogo string        `json:"merchant_logo,omitempty"`
	Goods        []InvoiceItem `json:"goods"`
	MCC          int           `json:"mcc_code"`
}

type InvoiceItem struct {
	Name string `json:"name"`
}
