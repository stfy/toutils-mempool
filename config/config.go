package config

import (
	"encoding/hex"
	"github.com/allisson/go-env"
)

var ZeroStateFileHash []byte

const (
	MainnetZeroStateFileHashStr = "5e994fcf4d425c0a6ce6a792594b7173205f740a39cd56f537defd28b48a0f6e"
)

func init() {
	var err error
	ZeroStateFileHash, err = hex.DecodeString(env.GetString("ZERO_STATE_FILE_HASH", MainnetZeroStateFileHashStr))
	if err != nil {
		panic(err)
	}
}
