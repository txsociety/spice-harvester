package core

import (
	"encoding/base64"
	"github.com/tonkeeper/tongo/abi"
	"github.com/tonkeeper/tongo/boc"
	"github.com/tonkeeper/tongo/tlb"
	"github.com/tonkeeper/tongo/ton"
)

func EncodePayload(invoice Invoice, adnlAddress *ton.Bits256) (string, error) {
	payload := abi.InvoicePayloadMsgBody{
		Id:  tlb.Bits128(invoice.ID),
		Url: abi.PaymentProviderUrl{SumType: "None"},
	}
	if adnlAddress != nil {
		payload.Url = abi.PaymentProviderUrl{
			SumType: "Tonsite",
			Tonsite: struct{ Address tlb.Bits256 }{Address: tlb.Bits256(*adnlAddress)},
		}
	}
	c := boc.NewCell()
	err := c.WriteUint(uint64(abi.InvoicePayloadMsgOpCode), 32)
	if err != nil {
		return "", err
	}
	err = tlb.Marshal(c, &payload)
	if err != nil {
		return "", err
	}
	bytes, err := c.ToBoc()
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}
