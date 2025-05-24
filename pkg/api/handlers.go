package api

import (
	"crypto/ed25519"
	"embed"
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

//go:embed static/*
var staticFiles embed.FS

type Handler struct {
	db               storage
	adnlAddress      *ton.Bits256
	paymentPrefixes  map[string]string
	currencies       map[string]core.ExtendedCurrency
	ourEncryptionKey ed25519.PrivateKey
	domain           string
}

func NewHandler(db storage, currencies map[string]core.ExtendedCurrency, adnlAddress *ton.Bits256, paymentPrefixes map[string]string, ourEncryptionKey ed25519.PrivateKey, domain string) *Handler {
	return &Handler{
		db:               db,
		currencies:       currencies,
		adnlAddress:      adnlAddress,
		paymentPrefixes:  paymentPrefixes,
		ourEncryptionKey: ourEncryptionKey,
		domain:           domain,
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
	if r.Body == nil {
		writeHttpError(w, "empty body", http.StatusBadRequest)
		return
	}
	var data NewInvoice
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		writeHttpError(w, "invalid invoice data: "+err.Error(), http.StatusBadRequest)
		return
	}
	recipient, err := h.db.GetRecipient(r.Context())
	if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	invoice, err := h.convertNewInvoice(data, recipient)
	if err != nil {
		writeHttpError(w, "invoice data parsing error: "+err.Error(), http.StatusBadRequest)
		return
	}
	err = h.db.CreateInvoice(r.Context(), *invoice)
	if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := core.ConvertInvoiceToPrintablePrivate(h.paymentPrefixes, *invoice, h.currencies, h.adnlAddress)
	if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoice", "error", err)
	}
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := core.ParseInvoiceID(r.PathValue("id"))
	if err != nil {
		writeHttpError(w, "invalid id", http.StatusBadRequest)
		return
	}
	invoice, err := h.db.GetInvoice(r.Context(), id)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		writeHttpError(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := core.ConvertInvoiceToPrintablePrivate(h.paymentPrefixes, invoice, h.currencies, h.adnlAddress)
	if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoice", "error", err)
	}
}

func (h *Handler) cancelInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := core.ParseInvoiceID(r.PathValue("id"))
	if err != nil {
		writeHttpError(w, "invalid id", http.StatusBadRequest)
		return
	}
	invoice, err := h.db.CancelInvoice(r.Context(), id)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		writeHttpError(w, "no waiting payment invoice found", http.StatusNotFound)
		return
	} else if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := core.ConvertInvoiceToPrintablePrivate(h.paymentPrefixes, invoice, h.currencies, h.adnlAddress)
	if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoice", "error", err)
	}
}

func (h *Handler) commitKey(w http.ResponseWriter, r *http.Request) {
	// TODO: or disable if adnl address is nil
	if r.Body == nil {
		writeHttpError(w, "empty body", http.StatusBadRequest)
		return
	}
	var data NewKey
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		writeHttpError(w, "invalid key data: "+err.Error(), http.StatusBadRequest)
		return
	}
	account, err := ton.ParseAccountID(r.PathValue("account"))
	if err != nil {
		writeHttpError(w, "invalid account: "+err.Error(), http.StatusBadRequest)
		return
	}
	encryptionKey, err := convertNewKey(account, data)
	if err != nil {
		writeHttpError(w, "key parsing error: "+err.Error(), http.StatusBadRequest)
		return
	}
	err = h.db.SaveEncryptionKey(r.Context(), account, encryptionKey)
	if err != nil {
		writeHttpError(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) getEncryptedData(w http.ResponseWriter, r *http.Request) {
	if h.ourEncryptionKey == nil {
		writeHttpError(w, "encrypted data is not available", http.StatusLocked)
		return
	}
	id, err := core.ParseInvoiceID(r.PathValue("id"))
	if err != nil {
		writeHttpError(w, "invalid id", http.StatusBadRequest)
		return
	}
	invoice, err := h.db.GetInvoice(r.Context(), id)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		writeHttpError(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		writeHttpError(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
	if invoice.Status != core.PaidInvoiceStatus {
		writeHttpError(w, "invalid invoice", http.StatusBadRequest)
		return
	}
	key, err := h.db.GetEncryptionKey(r.Context(), *invoice.PaidBy)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		writeHttpError(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		writeHttpError(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
	bytes, err := json.Marshal(invoice.Metadata)
	if err != nil {
		writeHttpError(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
	encrypted, err := encryptData(key, bytes, h.ourEncryptionKey)
	if err != nil {
		writeHttpError(w, core.ErrInternalServerError.Error(), http.StatusInternalServerError)
		return
	}
	// TODO: need to return salt (our address) or use adnl address as salt
	w.Header().Set("Content-Type", "application/octet-stream")
	_, err = w.Write(encrypted)
	if err != nil {
		slog.Error("encode data", "error", err)
	}
}

func (h *Handler) getInvoiceHistory(w http.ResponseWriter, r *http.Request) {
	var (
		limit int64          = 20
		after core.InvoiceID // empty ID
		err   error
	)
	if limitQuery := r.URL.Query().Get("limit"); len(limitQuery) > 0 {
		limit, err = strconv.ParseInt(limitQuery, 10, 64)
		if err != nil {
			writeHttpError(w, "invalid limit: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	if afterQuery := r.URL.Query().Get("after"); len(afterQuery) > 0 {
		id, err := core.ParseInvoiceID(afterQuery)
		if err != nil {
			writeHttpError(w, "invalid invoice ID: "+err.Error(), http.StatusBadRequest)
			return
		}
		_, err = h.db.GetInvoice(r.Context(), id)
		if err != nil && errors.Is(err, core.ErrNotFound) {
			writeHttpError(w, "unknown invoice ID", http.StatusBadRequest)
			return
		} else if err != nil {
			writeHttpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		after = id
	} // else: after = empty ID
	invoices, err := h.db.GetInvoices(r.Context(), after, limit)
	if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res := struct {
		Invoices []core.PrivateInvoicePrintable `json:"invoices"`
	}{
		Invoices: make([]core.PrivateInvoicePrintable, 0),
	}
	for _, inv := range invoices {
		invoice, err := core.ConvertInvoiceToPrintablePrivate(h.paymentPrefixes, inv, h.currencies, h.adnlAddress)
		if err != nil {
			writeHttpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		res.Invoices = append(res.Invoices, invoice)
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoices", "error", err)
	}
}

func (h *Handler) getInvoicePublic(w http.ResponseWriter, r *http.Request) {
	id, err := core.ParseInvoiceID(r.PathValue("id"))
	if err != nil {
		writeHttpError(w, "invalid id", http.StatusBadRequest)
		return
	}
	invoice, err := h.db.GetInvoice(r.Context(), id)
	if err != nil && errors.Is(err, core.ErrNotFound) {
		writeHttpError(w, err.Error(), http.StatusNotFound)
		return
	} else if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := core.ConvertInvoiceToPrintablePublic(h.paymentPrefixes, invoice, h.currencies, h.adnlAddress)
	if err != nil {
		writeHttpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode invoice", "error", err)
	}
}

func (h *Handler) getManifest(w http.ResponseWriter, r *http.Request) {
	res := struct {
		URL     string `json:"url"`
		Name    string `json:"name"`
		IconURL string `json:"iconUrl"`
	}{
		URL:     fmt.Sprintf("https://%s/", h.domain),
		Name:    "Payment",
		IconURL: fmt.Sprintf("https://%s/tonpay/public/static/logo.png", h.domain),
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(res)
	if err != nil {
		slog.Error("encode manifest", "error", err)
	}
}

func handleStatic(h http.Handler) http.Handler {
	// TODO: add recover?
	return http.StripPrefix("/tonpay/public", h)
}

func (h *Handler) getInvoiceRender(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, staticFiles, "static/[id].html")
}

func RegisterHandlers(mux *http.ServeMux, h *Handler, token string) {
	// private endpoints
	mux.HandleFunc("POST /tonpay/private/api/v1/invoice", recoverMiddleware(authMiddleware(h.createInvoice, token)))
	mux.HandleFunc("GET /tonpay/private/api/v1/invoices", recoverMiddleware(authMiddleware(h.getInvoiceHistory, token)))
	mux.HandleFunc("GET /tonpay/private/api/v1/invoices/{id}", recoverMiddleware(authMiddleware(h.getInvoice, token)))
	mux.HandleFunc("POST /tonpay/private/api/v1/invoices/{id}/cancel", recoverMiddleware(authMiddleware(h.cancelInvoice, token)))
	// public endpoints
	mux.HandleFunc("GET /tonpay/public/api/v1/invoices/{id}/metadata", recoverMiddleware(h.getEncryptedData))
	mux.HandleFunc("POST /tonpay/public/api/v1/keys/{account}/commit", recoverMiddleware(h.commitKey))
	mux.HandleFunc("GET /tonpay/public/api/v1/invoices/{id}", recoverMiddleware(h.getInvoicePublic))
	mux.HandleFunc("GET /tonpay/public/manifest", recoverMiddleware(h.getManifest))
	mux.Handle("GET /tonpay/public/static/", handleStatic(http.FileServerFS(staticFiles)))
	mux.HandleFunc("GET /tonpay/public/invoice/{id}", recoverMiddleware(h.getInvoiceRender))
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
		Currency:    cur.Currency,
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
	if len(signedEncryptionKey) != 64+4+32 { // 64 - sign, 4 - role, 32 - key
		return nil, errors.New("invalid encryption key len")
	}
	if string(signedEncryptionKey[64:64+4]) != "meta" {
		return nil, errors.New("invalid encryption key role")
	}
	if !ed25519.Verify(pubkey, signedEncryptionKey[64+4:], signedEncryptionKey[:64]) {
		return nil, errors.New("invalid encryption key signature")
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
