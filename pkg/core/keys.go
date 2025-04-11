package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/tonkeeper/tongo/ton"
	"golang.org/x/crypto/ed25519"
	"golang.org/x/crypto/pbkdf2"
)

func GetEncryptionKey(key string) (ed25519.PrivateKey, error) {
	return getKey(key, "meta")
}

func getKey(key string, salt string) (ed25519.PrivateKey, error) {
	b, err := hex.DecodeString(key)
	if err != nil {
		return nil, err
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes long")
	}
	seed := pbkdf2.Key(b, []byte(salt), 1, 32, sha256.New)
	privateKey := ed25519.NewKeyFromSeed(seed)
	return privateKey, nil
}

func GetAdnlAddress(key string) (ton.Bits256, error) {
	privateKey, err := getKey(key, "adnl")
	if err != nil {
		return ton.Bits256{}, err
	}
	h := sha256.New()
	h.Write([]byte{0xc6, 0xb4, 0x13, 0x48})
	h.Write(privateKey.Public().(ed25519.PublicKey))
	var res ton.Bits256
	copy(res[:], h.Sum(nil))
	return res, nil
}
