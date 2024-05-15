package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"io"
	"net/http"
	"os"
)

func handler(writer http.ResponseWriter, request *http.Request) {
	_, _ = writer.Write([]byte("Hello, " + request.URL.Query().Get("name") +
		"\nThis TON site works natively using tonutils-go!"))
}

var ZeroStateFileHash []byte

func init() {
	var err error
	ZeroStateFileHash, err = hex.DecodeString("5e994fcf4d425c0a6ce6a792594b7173205f740a39cd56f537defd28b48a0f6e")
	if err != nil {
		panic(err)
	}
}

func main() {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}

	gateway := adnl.NewGateway(priv)
	err = gateway.StartClient()
	if err != nil {
		panic(err)
	}

	dhtClient, err := dht.NewClientFromConfigUrl(context.Background(), gateway, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	//mx := http.NewServeMux()
	//mx.HandleFunc("/hello", handler)

	pk := loadKey()
	gate := adnl.NewGateway(pk)

	done := make(chan struct{})

	srv := NewServer(dhtClient, gate, pk)
	fmt.Println("start update dht ")
	srv.InitHandlers()

	go srv.log()
	go srv.updateDHTRecords(context.Background())
	go srv.startConn()

	<-done
}

func getPublicIP() string {
	req, err := http.Get("http://ip-api.com/json/")
	if err != nil {
		return err.Error()
	}
	defer req.Body.Close()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return err.Error()
	}

	var ip struct {
		Query string
	}
	_ = json.Unmarshal(body, &ip)

	return ip.Query
}

func loadKey() ed25519.PrivateKey {
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
	return ed25519.NewKeyFromSeed(dec)
}
