package keymanager

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
)

func LoadOrCreateKey(path string) ed25519.PrivateKey {
	file := "./key.txt"
	data, err := os.ReadFile(file)
	if err != nil {
		_, srvKey, err := ed25519.GenerateKey(nil)
		if err != nil {
			panic(err)
		}

		err = os.WriteFile(file, []byte(hex.EncodeToString(srvKey.Seed())), 777)
		if err != nil {
			panic(err)
		}

		return srvKey
	}

	dec, err := hex.DecodeString(string(data))
	if err != nil {
		panic(err)
	}

	return ed25519.NewKeyFromSeed(dec)
}
