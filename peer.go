package main

import (
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"reflect"
)

type PeerConnection struct {
	rldp *rldp.RLDP
	adnl adnl.Peer
}

func bootstrapPeer(peer adnl.Peer) *PeerConnection {

	peer.SetQueryHandler(func(msg *adnl.MessageQuery) error {
		req, over := overlay.UnwrapQuery(msg.Data)

		fmt.Println(reflect.TypeOf(req), "overlay::", hex.EncodeToString(over))

		return nil
	})
	peer.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
		req, over := overlay.UnwrapQuery(msg.Data)

		fmt.Println(reflect.TypeOf(req), "overlay::", hex.EncodeToString(over))

		return nil
	})

	//extADNL.SetOnUnknownOverlayQuery(func(query *adnl.MessageQuery) error {
	//	req, over := overlay.UnwrapQuery(query.Data)
	//
	//	fmt.Println("bootstrapPeer.extADNL.SetOnUnknownOverlayQuery", req, over)
	//
	//	return nil
	//})

	rl := rldp.NewClientV2(peer)
	extRl := overlay.CreateExtendedRLDP(rl)
	extRl.SetOnUnknownOverlayQuery(func(transferId []byte, query *rldp.Query) error {
		req, over := overlay.UnwrapQuery(query.Data)
		fmt.Println("bootstrapPeer.rl.SetOnUnknownOverlayQuery", req, over)

		return nil
	})

	extRl.SetOnDisconnect(func() {
		fmt.Println("disconnect")
	})

	return &PeerConnection{rldp: rl, adnl: peer}
}
