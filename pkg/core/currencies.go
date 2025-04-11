package core

import (
	"fmt"
	"github.com/tonkeeper/tongo/ton"
)

const DefaultTonTicker = "TON"

type Currency struct {
	// Do not use pointers to support equality
	Type         CurrencyType
	extraID      uint32
	jettonMaster ton.AccountID
}

type CurrencyType = string

const (
	TON    CurrencyType = "ton"
	Jetton CurrencyType = "jetton"
	Extra  CurrencyType = "extra"
)

func TonCurrency() Currency {
	return Currency{
		Type: TON,
	}
}

func ExtraCurrency(id uint32) Currency {
	return Currency{
		Type:    Extra,
		extraID: id,
	}
}

func JettonCurrency(master ton.AccountID) Currency {
	return Currency{
		Type:         Jetton,
		jettonMaster: master,
	}
}

func (c *Currency) Jetton() *ton.AccountID {
	if c.Type != Jetton {
		return nil
	}
	res := c.jettonMaster
	return &res
}

func (c *Currency) ExtraID() *uint32 {
	if c.Type != Extra {
		return nil
	}
	res := c.extraID
	return &res
}

func (c *Currency) String() string {
	switch c.Type {
	case TON:
		return fmt.Sprintf("%s$", c.Type)
	case Extra:
		return fmt.Sprintf("%s$%d", c.Type, c.extraID)
	case Jetton:
		return fmt.Sprintf("%s$%s", c.Type, c.jettonMaster.ToRaw())
	}
	return ""
}
