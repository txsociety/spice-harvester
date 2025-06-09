package config

import (
	"fmt"
	"github.com/caarlos0/env/v6"
	"github.com/tonkeeper/tongo/config"
	"github.com/tonkeeper/tongo/ton"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
)

type Config struct {
	Port            int                 `env:"PORT" envDefault:"8081"`
	LogLevel        slog.Level          `env:"LOG_LEVEL" envDefault:"INFO"`
	PostgresURI     string              `env:"POSTGRES_URI,required"`
	Token           string              `env:"TOKEN,required"`
	LiteServers     []config.LiteServer `env:"LITE_SERVERS"`
	Recipient       ton.AccountID       `env:"RECIPIENT,required"`
	Jettons         []jetton            `env:"JETTONS"`
	WebhookEndpoint string              `env:"WEBHOOK_ENDPOINT"`
	PaymentPrefixes prefixes            `env:"PAYMENT_PREFIXES"`
	Domain          string              `env:"DOMAIN"`
	// Key for generating a private key for metadata encryption and obtaining the adnl address of the proxy server
	Key        string `env:"KEY"` // 32 bytes in hex representation,
	Currencies map[string]core.ExtendedCurrency
}

type jetton struct {
	Address  ton.AccountID
	Ticker   string
	Decimals int
}

type prefixes map[string]string

func Load() Config {
	var (
		c  Config
		ll slog.Level
	)
	currencies := map[string]core.ExtendedCurrency{
		core.DefaultTonTicker: {Currency: core.TonCurrency()}, // TON tracking by default
	}
	c.PaymentPrefixes = make(prefixes)
	for name, prefix := range core.DefaultPaymentPrefixes {
		c.PaymentPrefixes[name] = prefix
	}
	if err := env.ParseWithFuncs(&c, map[reflect.Type]env.ParserFunc{
		reflect.TypeOf(ll): func(v string) (interface{}, error) {
			var level slog.Level
			err := level.UnmarshalText([]byte(v))
			return level, err
		},
		reflect.TypeOf([]config.LiteServer{}): func(v string) (interface{}, error) {
			servers, err := config.ParseLiteServersEnvVar(v)
			if err != nil {
				return nil, err
			}
			return servers, nil
		},
		reflect.TypeOf(ton.AccountID{}): func(v string) (interface{}, error) {
			addr, err := ton.ParseAccountID(v)
			if err != nil {
				return nil, err
			}
			return addr, nil
		},
		reflect.TypeOf([]jetton{}): func(v string) (interface{}, error) {
			var res []jetton
			addresses := make(map[ton.AccountID]struct{})
			for _, s := range strings.Split(v, ",") {
				vals := strings.Split(s, " ")
				if len(vals) != 3 {
					return nil, fmt.Errorf("invalid jetton config: %s", v)
				}
				addr, err := ton.ParseAccountID(vals[2])
				if err != nil {
					return nil, err
				}
				dec, err := strconv.Atoi(vals[1])
				if err != nil {
					return nil, err
				}
				if dec < 0 || dec > 255 {
					return nil, fmt.Errorf("invalid jetton decimals (must be 0..255): %s", vals[1])
				}
				ticker := vals[0]
				res = append(res, jetton{
					Address:  addr,
					Ticker:   ticker,
					Decimals: dec,
				})
				if _, ok := currencies[ticker]; ok {
					return nil, fmt.Errorf("duplicated jetton ticker: %s", v)
				}
				if _, ok := addresses[addr]; ok {
					return nil, fmt.Errorf("duplicated jetton address: %s", v)
				}
				addresses[addr] = struct{}{}
				currencies[ticker] = core.ExtendedCurrency{Currency: core.JettonCurrency(addr), JettonDecimals: dec}
			}
			return res, nil
		},
		reflect.TypeOf(prefixes{}): func(v string) (interface{}, error) {
			pref := c.PaymentPrefixes
			for _, s := range strings.Split(v, ",") {
				vals := strings.Split(s, " ")
				if len(vals) != 2 {
					return nil, fmt.Errorf("invalid prefixes config: %s", v)
				}
				pref[vals[0]] = vals[1]
			}
			return pref, nil
		},
	}); err != nil {
		panic("parse config error: " + err.Error())
	}
	c.Currencies = currencies
	return c
}
