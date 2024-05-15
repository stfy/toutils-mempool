package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/tlb"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"tonmempool/internal/keymanager"
	"tonmempool/internal/listener"
)

func main() {
	_, dhtKey, _ := ed25519.GenerateKey(nil)

	pk := keymanager.LoadOrCreateKey("./")

	dhtGateway := adnl.NewGateway(dhtKey)
	err := dhtGateway.StartClient()
	if err != nil {
		panic(err)
	}

	gateway := adnl.NewGateway(pk)
	gateway.SetExternalIP(net.ParseIP(getPublicIP()))

	dhtClient, err := dht.NewClientFromConfigUrl(context.Background(), dhtGateway, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	srv := listener.NewServer(dhtClient,
		gateway,
		pk,
		listener.WithOnExternalMessage(func(message tlb.ExternalMessage) error {

			fmt.Println("new message")

			return nil
		}),
	)
	go func() {
		err := srv.UpdateDHTRecords(context.Background())
		if err != nil {
			log.Fatal().Err(err).Msg("dht records can't updated")
		}
	}()

	srv.InitHandlers()
	go srv.LogKnownPeers()
	go srv.StartSearchingPeers()

	err = gateway.StartServer(":9056")
	if err != nil {
		log.Fatal().Err(err).Msg("cannot start gateway server")
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	<-sig
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
