package api

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tonkeeper/tongo/ton"
	"github.com/tonkeeper/tongo/toncrypto"
	"github.com/tonkeeper/tongo/wallet"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"time"
)

type Handler struct {
	db               storage
	adnlAddress      *ton.Bits256
	paymentPrefixes  map[string]string
	currencies       map[string]core.Currency
	ourEncryptionKey ed25519.PrivateKey
}

func NewHandler(db storage, currencies map[string]core.Currency, adnlAddress *ton.Bits256, paymentPrefixes map[string]string, ourEncryptionKey ed25519.PrivateKey) *Handler {
	return &Handler{
		db:               db,
		currencies:       currencies,
		adnlAddress:      adnlAddress,
		paymentPrefixes:  paymentPrefixes,
		ourEncryptionKey: ourEncryptionKey,
	}
}

type NewInvoice struct {
	Amount      string                     `json:"amount"`
	Currency    string                     `json:"currency"`
	LifeTime    int64                      `json:"life_time"`
	PrivateInfo map[string]json.RawMessage `json:"private_info,omitempty"`
	Metadata    core.InvoiceMetadata       `json:"metadata"`
}

type NewKey struct {
	WalletVersion       string `json:"wallet_version"`
	PublicKey           string `json:"public_key"`
	SignedEncryptionKey string `json:"signed_encryption_key"`
}

func (h *Handler) createInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Body == nil {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	var data NewInvoice
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		http.Error(w, "invalid invoice data: "+err.Error(), http.StatusBadRequest)
		return
	}
	recipient, err := h.db.GetRecipient(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	invoice, err := h.convertNewInvoice(data, recipient)
	if err != nil {
		http.Error(w, "invoice data parsing error: "+err.Error(), http.StatusBadRequest)
		return
	}
	err = h.db.CreateInvoice(r.Context(), *invoice)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := core.ConvertInvoiceToPrintable(h.paymentPrefixes, *invoice, h.currencies, h.adnlAddress)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoice", "error", err)
	}
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id, err := core.ParseInvoiceID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	invoice, err := h.db.GetInvoice(r.Context(), id)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := core.ConvertInvoiceToPrintable(h.paymentPrefixes, invoice, h.currencies, h.adnlAddress)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoice", "error", err)
	}
}

func (h *Handler) cancelInvoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id, err := core.ParseInvoiceID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	invoice, err := h.db.CancelInvoice(r.Context(), id)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		http.Error(w, "no waiting payment invoice found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := core.ConvertInvoiceToPrintable(h.paymentPrefixes, invoice, h.currencies, h.adnlAddress)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoice", "error", err)
	}
}

func (h *Handler) commitKey(w http.ResponseWriter, r *http.Request) {
	// TODO: or disable if adnl address is nil
	if r.Body == nil {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	var data NewKey
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		http.Error(w, "invalid key data: "+err.Error(), http.StatusBadRequest)
		return
	}
	account, err := ton.ParseAccountID(r.PathValue("account"))
	if err != nil {
		http.Error(w, "invalid account: "+err.Error(), http.StatusBadRequest)
		return
	}
	encryptionKey, err := convertNewKey(account, data)
	if err != nil {
		http.Error(w, "key parsing error: "+err.Error(), http.StatusBadRequest)
		return
	}
	err = h.db.SaveEncryptionKey(r.Context(), account, encryptionKey)
	if err != nil {
		http.Error(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) getEncryptedData(w http.ResponseWriter, r *http.Request) {
	if h.ourEncryptionKey == nil {
		http.Error(w, "encrypted data is not available", http.StatusLocked)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	id, err := core.ParseInvoiceID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	invoice, err := h.db.GetInvoice(r.Context(), id)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
	if invoice.Status != core.PaidInvoiceStatus {
		http.Error(w, "invalid invoice", http.StatusBadRequest)
		return
	}
	key, err := h.db.GetEncryptionKey(r.Context(), *invoice.PaidBy)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
	bytes, err := json.Marshal(invoice.Metadata)
	if err != nil {
		http.Error(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
	encrypted, err := encryptData(key, bytes, h.ourEncryptionKey)
	if err != nil {
		http.Error(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
	// TODO: need to return salt (our address) or use adnl address as salt
	_, err = w.Write(encrypted)
	if err != nil {
		http.Error(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		slog.Error("encode data", "error", err)
	}
}

func (h *Handler) getInvoiceHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var (
		limit int64          = 20
		after core.InvoiceID // empty ID
		err   error
	)
	if limitQuery := r.URL.Query().Get("limit"); len(limitQuery) > 0 {
		limit, err = strconv.ParseInt(limitQuery, 10, 64)
		if err != nil {
			http.Error(w, "invalid limit: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	if afterQuery := r.URL.Query().Get("after"); len(afterQuery) > 0 {
		id, err := core.ParseInvoiceID(afterQuery)
		if err != nil {
			http.Error(w, "invalid invoice ID: "+err.Error(), http.StatusBadRequest)
			return
		}
		_, err = h.db.GetInvoice(r.Context(), id)
		if err != nil && errors.Is(err, core.ErrNotFound) {
			http.Error(w, "unknown invoice ID", http.StatusBadRequest)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		after = id
	} // else: after = empty ID
	invoices, err := h.db.GetInvoices(r.Context(), after, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res := struct {
		Invoices []core.InvoicePrintable `json:"invoices"`
	}{
		Invoices: make([]core.InvoicePrintable, 0),
	}
	for _, inv := range invoices {
		invoice, err := core.ConvertInvoiceToPrintable(h.paymentPrefixes, inv, h.currencies, h.adnlAddress)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res.Invoices = append(res.Invoices, invoice)
	}
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoices", "error", err)
	}
}

func (h *Handler) invoices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getInvoiceHistory(w, r)
	case http.MethodPost:
		h.createInvoice(w, r)
	default:
		writeHttpError(w, http.StatusMethodNotAllowed, "only POST or GET method is supported")
		return
	}
}

func RegisterHandlers(mux *http.ServeMux, h *Handler, token string) {
	mux.HandleFunc("/v1/invoices", recoverMiddleware(authMiddleware(h.invoices, token)))
	mux.HandleFunc("/v1/invoices/{id}", recoverMiddleware(authMiddleware(get(h.getInvoice), token)))
	mux.HandleFunc("/v1/invoices/{id}/cancel", recoverMiddleware(authMiddleware(post(h.cancelInvoice), token)))
	mux.HandleFunc("/v1/invoices/{id}/metadata", recoverMiddleware(get(h.getEncryptedData))) // public endpoint
	mux.HandleFunc("/v1/keys/{account}/commit", recoverMiddleware(post(h.commitKey)))        // public endpoint
}

func (h *Handler) convertNewInvoice(newInvoice NewInvoice, recipient ton.AccountID) (*core.Invoice, error) {
	amount, ok := new(big.Int).SetString(newInvoice.Amount, 10)
	if !ok {
		return nil, errors.New("can not parse amount string")
	}
	if amount.Cmp(big.NewInt(0)) != 1 {
		return nil, errors.New("amount must be positive integer")
	}
	if newInvoice.LifeTime <= 0 {
		return nil, errors.New("life time must be positive integer")
	}
	now := time.Now()
	cur, ok := h.currencies[newInvoice.Currency]
	if !ok {
		return nil, fmt.Errorf("currency ticker %s not found", newInvoice.Currency)
	}
	res := core.Invoice{
		ID:          core.NewInvoiceID(),
		Status:      core.WaitingInvoiceStatus,
		Amount:      amount,
		Currency:    cur,
		CreatedAt:   now,
		ExpireAt:    now.Add(time.Second * time.Duration(newInvoice.LifeTime)),
		PrivateInfo: newInvoice.PrivateInfo,
		UpdatedAt:   now,
		Overpayment: big.NewInt(0),
		Recipient:   recipient,
	}
	if newInvoice.Metadata.Goods == nil {
		newInvoice.Metadata.Goods = make([]core.InvoiceItem, 0)
	}
	err := validateMetadata(newInvoice.Metadata)
	if err != nil {
		return nil, fmt.Errorf("metadata validation: %w", err)
	}
	metaBytes, err := json.Marshal(newInvoice.Metadata)
	if err != nil {
		return nil, fmt.Errorf("can not marshal metadata: %w", err)
	}
	var raw map[string]json.RawMessage
	err = json.Unmarshal(metaBytes, &raw)
	if err != nil {
		return nil, fmt.Errorf("can not unmarshal metadata: %w", err)
	}
	res.Metadata = raw
	if res.PrivateInfo == nil {
		res.PrivateInfo = make(map[string]json.RawMessage)
	}
	return &res, nil
}

func convertNewKey(account ton.AccountID, newKey NewKey) ([]byte, error) {
	pubkey, err := hex.DecodeString(newKey.PublicKey)
	if err != nil {
		return nil, err
	}
	if len(pubkey) != 32 {
		return nil, errors.New("invalid public key")
	}
	ver, err := wallet.VersionFromString(newKey.WalletVersion)
	if err != nil {
		return nil, err
	}
	addr, err := wallet.GenerateWalletAddress(pubkey, ver, nil, 0, nil)
	if err != nil {
		return nil, err
	}
	if addr != account {
		return nil, errors.New("invalid public key")
	}
	// wallet public key valid for account
	signedEncryptionKey, err := hex.DecodeString(newKey.SignedEncryptionKey)
	if err != nil {
		return nil, err
	}
	if len(signedEncryptionKey) != 64+32 {
		return nil, errors.New("invalid encryption key")
	}
	if !ed25519.Verify(pubkey, signedEncryptionKey[64:], signedEncryptionKey[:64]) {
		return nil, errors.New("invalid encryption key")
	}
	return signedEncryptionKey[64:], nil
}

func validateMetadata(meta core.InvoiceMetadata) error {
	if len(meta.MerchantName) == 0 {
		return errors.New("missing merchant_name")
	}
	if meta.MCC < 0 || meta.MCC > 9999 {
		return errors.New("mcc_code must be between 0 and 9999")
	}
	return nil
}

func encryptData(receiverPubkey []byte, data []byte, ourEncryptionKey ed25519.PrivateKey) ([]byte, error) {
	acc := ton.MustParseAccountID("0:0") // TODO: clarify salt
	salt := []byte(acc.ToHuman(true, false))
	return toncrypto.Encrypt(receiverPubkey, ourEncryptionKey, data, salt)
}
