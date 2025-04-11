package core

import (
	"github.com/tonkeeper/tongo/tlb"
	"github.com/tonkeeper/tongo/ton"
)

type AccountInfo struct {
	MaxDepthLt uint64
	Recipient  ton.AccountID
	Jetton     *ton.AccountID
}

type TxID struct {
	Lt   uint64
	Hash ton.Bits256
}

type TxGap struct {
	StartLt, EndLt uint64
	StartHash      ton.Bits256
}

type Account struct {
	AccountID ton.AccountID
	Info      AccountInfo
}

type Transaction struct {
	Lt          uint64
	Hash        ton.Bits256
	PrevTxLt    uint64
	PrevTxHash  ton.Bits256
	Utime       uint32
	Success     bool
	InMessage   Message
	OutMessages []Message
}

type Message struct {
	Type             string
	Source           *ton.AccountID
	Destination      *ton.AccountID
	Value            uint64
	ExtraCurrencies  map[uint32]tlb.VarUInteger32
	Lt               uint64
	Hash             ton.Bits256
	BodyHash         ton.Bits256
	DecodedOperation string
	DecodedBody      any
}
