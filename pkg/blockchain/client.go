package blockchain

import (
	"context"
	"errors"
	"fmt"
	"github.com/tonkeeper/tongo"
	"github.com/tonkeeper/tongo/abi"
	"github.com/tonkeeper/tongo/boc"
	tongoCode "github.com/tonkeeper/tongo/code"
	"github.com/tonkeeper/tongo/config"
	"github.com/tonkeeper/tongo/liteapi"
	"github.com/tonkeeper/tongo/tlb"
	"github.com/tonkeeper/tongo/ton"
	"github.com/tonkeeper/tongo/tvm"
	"github.com/tonkeeper/tongo/txemulator"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Client struct {
	connection *liteapi.Client

	lastMasterchainBlockLock sync.RWMutex
	lastMasterchainBlock     *ton.BlockIDExt
}

type storage interface {
	SetLastTrustedBlock(ctx context.Context, block ton.BlockIDExt) error
	GetLastTrustedBlock(ctx context.Context) (*ton.BlockIDExt, error)
}

func New(ls []config.LiteServer) (*Client, error) {
	options := make([]liteapi.Option, 0)
	if len(ls) > 0 {
		options = append(options, liteapi.WithLiteServers(ls))
		options = append(options, liteapi.WithMaxConnectionsNumber(len(ls)))
	} else {
		options = append(options, liteapi.Mainnet())
		slog.Warn("liteservers are not set, retrieving liteservers from global config")
	}
	// TODO: set trustedBlock
	api, err := liteapi.NewClient(options...)
	if err != nil {
		return nil, err
	}
	c := &Client{
		connection: api,
	}
	return c, nil
}

func (c *Client) RunBlockWatcher(ctx context.Context, storage storage, wg *sync.WaitGroup) {
	slog.Info("initializing client. Can require few minutes for checking proofs")
	wait := make(chan struct{})
	go c.runBlockWatcher(ctx, storage, wg, wait)
	<-wait
	slog.Info("client initialized")
}

func (c *Client) runBlockWatcher(ctx context.Context, storage storage, wg *sync.WaitGroup, wait chan struct{}) {
	slog.Info("block watcher started")
	wg.Add(1)
	defer wg.Done()

	initialized := false
	for {
		if !initialized {
			err := c.updateMasterchainBlock(ctx, storage, 10*time.Minute)
			if err != nil {
				slog.Error("can not get proofed block", "err", err.Error())
				time.Sleep(2 * time.Second)
				continue
			}
			initialized = true
			close(wait)
		}
		select {
		case <-ctx.Done():
			slog.Info("block watcher stopped")
			return
		case <-time.After(5 * time.Second):
			err := c.updateMasterchainBlock(ctx, storage, 10*time.Minute)
			if err != nil {
				slog.Error("can not update block", "err", err.Error())
			}
		}
	}
}

func (c *Client) updateMasterchainBlock(ctx context.Context, storage storage, timeout time.Duration) error {
	ctx1, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	info, err := c.connection.GetMasterchainInfo(ctx1)
	if err != nil {
		return fmt.Errorf("can not get masterchain info: %w", err)
	}
	block := info.Last.ToBlockIdExt()
	c.lastMasterchainBlockLock.Lock()
	c.lastMasterchainBlock = &block
	c.lastMasterchainBlockLock.Unlock()
	err = storage.SetLastTrustedBlock(ctx1, block)
	if err != nil {
		return fmt.Errorf("can not save last block: %w", err)
	}
	return nil
}

func (c *Client) getLastMasterchainBlock() (ton.BlockIDExt, error) {
	c.lastMasterchainBlockLock.RLock()
	defer c.lastMasterchainBlockLock.RUnlock()
	if c.lastMasterchainBlock == nil {
		return ton.BlockIDExt{}, errors.New("blockchain client not initialized")
	}
	return *c.lastMasterchainBlock, nil
}

func (c *Client) GetTransactions(ctx context.Context, a ton.AccountID, lt, maxDepthLt uint64, hash ton.Bits256) ([]core.Transaction, error) {
	var transactions []core.Transaction
	txs, err := c.connection.GetTransactions(ctx, 16, a, lt, hash)
	if err != nil {
		return nil, err
	}
	for _, tx := range txs {
		if ton.Bits256(tx.Hash()) != hash {
			return nil, fmt.Errorf("mismatched tx hash")
		}
		if tx.Lt <= maxDepthLt {
			break
		}
		hash = ton.Bits256(tx.PrevTransHash)
		lt = tx.PrevTransLt
		transaction := convertTransaction(tx)
		transactions = append(transactions, transaction)
	}
	return transactions, nil
}

func (c *Client) GetAccountState(ctx context.Context, accountID ton.AccountID) (tlb.ShardAccount, uint32, error) {
	block, err := c.getLastMasterchainBlock()
	if err != nil {
		return tlb.ShardAccount{}, 0, err
	}
	shardAcc, err := c.connection.WithBlock(block).GetAccountState(ctx, accountID)
	if err != nil {
		return tlb.ShardAccount{}, 0, err
	}
	return shardAcc, block.Seqno, nil
}

func (c *Client) GetLibraries(ctx context.Context, libraryList []ton.Bits256) (map[ton.Bits256]*boc.Cell, error) {
	return c.connection.GetLibraries(ctx, libraryList)
}

func (c *Client) RunSmcMethodByID(ctx context.Context, accountID ton.AccountID, methodID int, params tlb.VmStack) (uint32, tlb.VmStack, error) {
	state, _, err := c.GetAccountState(ctx, accountID)
	if err != nil {
		return 0, nil, err
	}
	if state.Account.Status() != tlb.AccountActive {
		return 0, nil, errors.New("account is not active")
	}
	var (
		code, data *boc.Cell
	)
	if !state.Account.Account.Storage.State.AccountActive.StateInit.Code.Exists {
		return 0, nil, errors.New("account code is empty")
	}
	if !state.Account.Account.Storage.State.AccountActive.StateInit.Data.Exists {
		return 0, nil, errors.New("account data is empty")
	}
	code = &state.Account.Account.Storage.State.AccountActive.StateInit.Code.Value.Value
	data = &state.Account.Account.Storage.State.AccountActive.StateInit.Data.Value.Value

	cfg := boc.NewCell()
	configParams, err := c.connection.GetConfigAll(ctx, 0)
	if err != nil {
		return 0, nil, err
	}
	if err := tlb.Marshal(cfg, configParams.Config); err != nil {
		return 0, nil, err
	}

	libs := map[tongo.Bits256]*boc.Cell{}
	accountLibs := state.Account.Account.Storage.State.AccountActive.StateInit.Library
	for _, item := range accountLibs.Items() {
		libs[tongo.Bits256(item.Key)] = &item.Value.Root
	}

	libHashes, err := tongoCode.FindLibraries(code)
	if err != nil {
		return 0, nil, err
	}
	if len(libHashes) > 0 {
		publicLibs, err := c.GetLibraries(ctx, libHashes)
		if err != nil {
			return 0, nil, err
		}
		for hash, lib := range publicLibs {
			libs[hash] = lib
		}
	}
	base64libs, err := tongoCode.LibrariesToBase64(libs)
	if err != nil {
		return 0, nil, err
	}

	emulator, err := tvm.NewEmulator(code, data, cfg,
		tvm.WithVerbosityLevel(txemulator.LogTruncated),
		tvm.WithLibrariesBase64(base64libs))
	if err != nil {
		return 0, tlb.VmStack{}, err
	}
	err = emulator.SetGasLimit(10_000_000)
	if err != nil {
		return 0, tlb.VmStack{}, err
	}
	return emulator.RunSmcMethodByID(ctx, accountID, methodID, params)
}

func (c *Client) getJettonWallet(ctx context.Context, jettonMaster, owner ton.AccountID) (ton.AccountID, error) {
	_, resp, err := abi.GetWalletAddress(ctx, c, jettonMaster, owner.ToMsgAddress())
	if err != nil {
		return ton.AccountID{}, fmt.Errorf("can not get jetton wallet address: %w", err)
	}
	body, ok := resp.(abi.GetWalletAddressResult)
	if !ok {
		return ton.AccountID{}, errors.New("invalid response for get_wallet_address")
	}
	wallet, err := ton.AccountIDFromTlb(body.JettonWalletAddress)
	if err != nil {
		return ton.AccountID{}, fmt.Errorf("invalid jetton wallet account id: %w", err)
	}
	if wallet == nil {
		return ton.AccountID{}, errors.New("jetton wallet account is none")
	}
	return *wallet, nil
}

func (c *Client) getJettonData(ctx context.Context, account ton.AccountID) (ton.AccountID, ton.AccountID, error) {
	_, resp, err := abi.GetWalletData(ctx, c, account)
	if err != nil {
		return ton.AccountID{}, ton.AccountID{}, fmt.Errorf("can not get jetton data: %w", err)
	}
	body, ok := resp.(abi.GetWalletDataResult)
	if !ok {
		return ton.AccountID{}, ton.AccountID{}, errors.New("invalid response for get_wallet_data")
	}
	jetton, err := ton.AccountIDFromTlb(body.Jetton)
	if err != nil {
		return ton.AccountID{}, ton.AccountID{}, fmt.Errorf("invalid jetton account id: %w", err)
	}
	if jetton == nil {
		return ton.AccountID{}, ton.AccountID{}, errors.New("jetton account is none")
	}
	owner, err := ton.AccountIDFromTlb(body.Owner)
	if err != nil {
		return ton.AccountID{}, ton.AccountID{}, fmt.Errorf("invalid owner account id: %w", err)
	}
	if owner == nil {
		return ton.AccountID{}, ton.AccountID{}, errors.New("owner account is none")
	}
	return *jetton, *owner, nil
}

// GetJettonWallet calculates the wallet address and validates it if it deployed.
func (c *Client) GetJettonWallet(ctx context.Context, jettonMaster, owner ton.AccountID) (ton.AccountID, error) {
	jWallet, err := c.getJettonWallet(ctx, jettonMaster, owner)
	if err != nil {
		return ton.AccountID{}, err
	}
	state, _, err := c.GetAccountState(ctx, jWallet)
	if err != nil {
		return ton.AccountID{}, fmt.Errorf("can not get account state: %w", err)
	}
	if state.Account.Status() != tlb.AccountActive {
		slog.Warn("jetton wallet is not deployed. It cannot be verified that it refers to the correct Jetton master", "account", jWallet.ToRaw())
		return jWallet, nil
	}
	master, walletOwner, err := c.getJettonData(ctx, jWallet)
	if err != nil {
		return ton.AccountID{}, err
	}
	if master != jettonMaster {
		return ton.AccountID{}, errors.New("jetton master from Jetton wallet is not equal to Jetton master")
	}
	if owner != walletOwner {
		return ton.AccountID{}, errors.New("wallet owner from jetton wallet is not equal to owner")
	}
	return jWallet, nil
}

func convertTransaction(tx ton.Transaction) core.Transaction {
	transaction := core.Transaction{
		Lt:         tx.Lt,
		Hash:       ton.Bits256(tx.Hash()),
		PrevTxHash: ton.Bits256(tx.PrevTransHash),
		PrevTxLt:   tx.PrevTransLt,
		Utime:      tx.Now,
		Success:    tx.IsSuccess(),
	}
	if tx.Transaction.Msgs.InMsg.Exists {
		transaction.InMessage = convertMessage(tx.Transaction.Msgs.InMsg.Value.Value, abi.IUnknown)
	}
	for _, m := range tx.Transaction.Msgs.OutMsgs.Values() {
		message := convertMessage(m.Value, abi.IUnknown)
		transaction.OutMessages = append(transaction.OutMessages, message)
	}
	return transaction
}

func convertMessage(m tlb.Message, accountInterface abi.ContractInterface) core.Message {
	message := core.Message{
		Type:            strings.TrimSuffix(string(m.Info.SumType), "MsgInfo"),
		Hash:            ton.Bits256(m.Hash(true)),
		ExtraCurrencies: make(map[uint32]tlb.VarUInteger32),
	}
	switch m.Info.SumType {
	case "IntMsgInfo":
		a, _ := ton.AccountIDFromTlb(m.Info.IntMsgInfo.Src)
		message.Source = a
		a, _ = ton.AccountIDFromTlb(m.Info.IntMsgInfo.Dest)
		message.Destination = a
		message.Value = uint64(m.Info.IntMsgInfo.Value.Grams) + uint64(m.Info.IntMsgInfo.IhrFee)
		for _, item := range m.Info.IntMsgInfo.Value.Other.Dict.Items() {
			message.ExtraCurrencies[uint32(item.Key)] = item.Value
		}
		message.Lt = m.Info.IntMsgInfo.CreatedLt
	case "ExtInMsgInfo":
		a, _ := ton.AccountIDFromTlb(m.Info.ExtInMsgInfo.Dest)
		message.Destination = a
	case "ExtOutMsgInfo":
		a, _ := ton.AccountIDFromTlb(m.Info.ExtOutMsgInfo.Src)
		message.Source = a
		message.Lt = m.Info.ExtOutMsgInfo.CreatedLt
	}
	if m.Info.SumType == "ExtOutMsgInfo" { //skip. nothing interesting
		return message
	}
	body := boc.Cell(m.Body.Value)
	if body.BitsAvailableForRead()+body.RefsAvailableForRead() == 0 {
		return message //empty body
	}
	b := body.CopyRemaining()
	h, err := b.Hash()
	if err != nil {
		return message
	}
	message.BodyHash = ton.Bits256(h)
	_, decodedOperation, decodedBody, _ := abi.InternalMessageDecoder(b, []abi.ContractInterface{accountInterface})
	if decodedOperation != nil {
		message.DecodedOperation = *decodedOperation
		message.DecodedBody = decodedBody
	}
	return message
}
