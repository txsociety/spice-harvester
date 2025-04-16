package main

import (
	"context"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/blockchain"
	"github.com/txsociety/spice-harvester/pkg/core"
	"github.com/txsociety/spice-harvester/pkg/db"
)

func getAccountsForTracking(ctx context.Context, dbClient *db.Connection, bcClient *blockchain.Client, recipient ton.AccountID, currencies map[string]core.ExtendedCurrency) (map[ton.AccountID]core.AccountInfo, error) {
	accounts, err := dbClient.GetTrackedAccounts(ctx, recipient, currencies)
	if err != nil {
		return nil, err
	}
	newAccounts := make(map[ton.AccountID]core.AccountInfo)
	for _, cur := range currencies {
		switch cur.Type {
		case core.Extra: // same as TON account
			continue
		case core.TON:
			_, ok := accounts[recipient]
			if !ok {
				newAcc := core.AccountInfo{
					Recipient: recipient,
				}
				accounts[recipient] = newAcc
				newAccounts[recipient] = newAcc
			}
			continue
		}
		found := false
		for _, account := range accounts {
			if account.Recipient != recipient || account.Jetton == nil || *account.Jetton != *cur.Jetton() {
				continue
			}
			found = true
		}
		if !found {
			jettonWallet, err := bcClient.GetJettonWallet(ctx, *cur.Jetton(), recipient)
			if err != nil {
				return nil, err
			}
			newAcc := core.AccountInfo{
				Recipient: recipient,
				Jetton:    cur.Jetton(),
			}
			accounts[jettonWallet] = newAcc
			newAccounts[jettonWallet] = newAcc
		}
	}
	for acc, info := range newAccounts {
		state, _, err := bcClient.GetAccountState(ctx, acc)
		if err != nil {
			return nil, err
		}
		txID := core.TxID{
			Lt:   state.LastTransLt,
			Hash: ton.Bits256(state.LastTransHash),
		}
		info.MaxDepthLt = txID.Lt // set last LT as start LT for new accounts
		accounts[acc] = info
		err = dbClient.CreateAccount(ctx, core.Account{
			AccountID: acc,
			Info:      info,
		}, txID)
		if err != nil {
			return nil, err
		}
	}
	return accounts, nil
}
