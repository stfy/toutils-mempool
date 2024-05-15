package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/adnl/node"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	rldphttp "github.com/xssnick/tonutils-go/adnl/rldp/http"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"net"
	"reflect"
	"sync"
	"time"
)

type Server struct {
	mtx *sync.Mutex

	key  ed25519.PrivateKey
	dht  *dht.Client
	gate *adnl.Gateway

	myself     *overlay.Node
	overlayId  []byte
	knownPeers map[string]overlay.Node
}

func NewServer(
	dhtClient *dht.Client,
	gateway *adnl.Gateway,
	key ed25519.PrivateKey,
) *Server {
	state := node.ShardPublicOverlayID{Shard: -9223372036854775808, Workchain: 0, ZeroStateFileHash: ZeroStateFileHash}
	stateHash, _ := tl.Hash(state)
	keyHash, _ := tl.Hash(adnl.PublicKeyOverlay{
		Key: stateHash,
	})

	return &Server{
		mtx:        &sync.Mutex{},
		dht:        dhtClient,
		gate:       gateway,
		key:        key,
		knownPeers: make(map[string]overlay.Node),
		overlayId:  keyHash,
	}
}

func (s *Server) updateDHTRecords(ctx context.Context) error {
	addr := s.gate.GetAddressList()

	ctxStore, cancel := context.WithTimeout(ctx, 90*time.Second)
	stored, id, err := s.dht.StoreAddress(ctxStore, addr, 10*time.Minute, s.key, 8)
	cancel()
	if err != nil && stored == 0 {
		return err
	}

	// make sure it was saved
	_, _, err = s.dht.FindAddresses(ctx, id)
	if err != nil {
		return err
	}

	fmt.Println("[STORAGE_DHT] OUR NODE ADDRESS UPDATED ON", stored, "NODES")

	return nil
}

func (s *Server) getKnownPeers() overlay.NodesList {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var al overlay.NodesList

	al.List = append(al.List, *s.myself)
	if len(s.knownPeers) <= 4 {
		for _, v := range s.knownPeers {
			al.List = append(al.List, v)
		}
	} else {
		// TODO: random choice
		overlayNodes := make([]overlay.Node, 0)

		for _, v := range s.knownPeers {
			overlayNodes = append(overlayNodes, v)
		}

		al.List = append(al.List, overlayNodes[0:4]...)
	}

	return al
}

func (s *Server) addKnownPeer(addrs string, client overlay.Node) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.knownPeers[addrs] = client
}

func (s *Server) hasPeer(addrs string) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	_, ok := s.knownPeers[addrs]

	return ok
}

func (s *Server) log() {
	for {
		fmt.Println("known_peers", len(s.knownPeers))
		time.Sleep(time.Second)
	}
}

func (s *Server) OverlayId() ([]byte, []byte) {
	state := node.ShardPublicOverlayID{Shard: -9223372036854775808, Workchain: 0, ZeroStateFileHash: ZeroStateFileHash}
	publicOverlayId, _ := tl.Hash(state)

	keyHash, _ := tl.Hash(adnl.PublicKeyOverlay{
		Key: publicOverlayId,
	})

	return keyHash, publicOverlayId
}

func (s *Server) InitHandlers() {
	overlayId, _ := s.OverlayId()

	s.gate.SetExternalIP(net.ParseIP(getPublicIP()))
	s.gate.SetConnectionHandler(func(client adnl.Peer) error {
		adnlAddr, err := rldphttp.SerializeADNLAddress(client.GetID())
		if err != nil {
			return err
		}

		fmt.Println("gate connect", adnlAddr)

		adnlPeer := overlay.CreateExtendedADNL(client)
		ovr := adnlPeer.WithOverlay(overlayId)
		ovr.SetBroadcastHandler(func(msg tl.Serializable, trusted bool) error {
			fmt.Println("broadcast handler", trusted, msg)

			return nil
		})

		ovr.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
			fmt.Println("msg data", reflect.TypeOf(msg.Data))

			req, over := overlay.UnwrapQuery(msg.Data)

			switch v := msg.Data.(type) {
			case overlay.Broadcast:
				var gh any
				_, _ = tl.Parse(&gh, v.Data, true)

				fmt.Println("HANDLE NewExternalMessageBroadcast", hex.EncodeToString(over))

				switch externalMsg := gh.(type) {
				case node.NewExternalMessageBroadcast:
					var extMsg tlb.ExternalMessage
					extCell, err := cell.FromBOC(externalMsg.Message.Data)
					if err != nil {
						fmt.Println(err)
						return nil
					}

					if err := tlb.LoadFromCell(&extMsg, extCell.BeginParse()); err != nil {
						fmt.Println(err)
						return nil
					}

					fmt.Println("DST ADDR::", extMsg.DstAddr, "received ::", time.Now())
					fmt.Println("TX ID::", hex.EncodeToString(extCell.Hash()))

					opcode, err := extMsg.Body.BeginParse().LoadUInt(32)
					fmt.Println("opcode", fmt.Sprintf("%x", opcode))
				default:
					return nil
				}

				fmt.Println(reflect.TypeOf(gh))
			default:

				fmt.Println("default handler", reflect.TypeOf(v))
				return nil
			}

			fmt.Println("RECEIVE BROADCAST", reflect.TypeOf(req))
			fmt.Println(msg.Data, req, over)

			return nil
		})

		adnlPeer.SetQueryHandler(func(msg *adnl.MessageQuery) error {
			req, over := overlay.UnwrapQuery(msg.Data)

			fmt.Println("extADNL.SetOnUnknownOverlayQuery", reflect.TypeOf(req), "overlay", hex.EncodeToString(over))

			return nil
		})
		adnlPeer.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
			req, over := overlay.UnwrapQuery(msg.Data)

			fmt.Println("extADNL.SetOnUnknownOverlayQuery", reflect.TypeOf(req), "overlay", hex.EncodeToString(over))

			return nil
		})

		rlClient := rldp.NewClientV2(adnlPeer)
		rl := overlay.CreateExtendedRLDP(rlClient)
		rl.SetOnUnknownOverlayQuery(func(transferId []byte, query *rldp.Query) error {
			req, over := overlay.UnwrapQuery(query.Data)
			fmt.Println("rl.SetOnUnknownOverlayQuery", req, over)

			return nil
		})

		return nil
	})

	err := s.gate.StartServer(":9056")
	if err != nil {
		panic(err)
	}
}

func (s *Server) connectPeer(overlayNode overlay.Node) error {

	nodeId, _ := tl.Hash(overlayNode.ID)
	addrs, keyN, err := s.dht.FindAddresses(context.Background(), nodeId)
	if err != nil {
		fmt.Println(err)
		return err
	}

	addr := addrs.Addresses[0].IP.String() + ":" + fmt.Sprint(addrs.Addresses[0].Port)
	if s.hasPeer(addr) {
		fmt.Println("known peer skip", addr)
		return nil
	}

	ax, err := s.gate.RegisterClient(addr, keyN)

	ax.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
		req, _ := overlay.UnwrapQuery(msg.Data)

		switch v := req.(type) {
		case overlay.GetRandomPeers:

			if err != nil {
				return err
			}
			break
		case node.NewExternalMessageBroadcast:

			fmt.Println("new external message", v.Message)
		default:
			fmt.Println(v)
		}

		return nil
	})
	if err != nil {
		fmt.Println(fmt.Errorf("failed to connnect to node: %w", err))
	}

	//_ = bootstrapPeer(ax)

	fmt.Println("connect", addr)

	var al overlay.NodesList
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)

	err = ax.Query(ctx, overlay.WrapQuery(overlayNode.Overlay, &overlay.GetRandomPeers{List: s.getKnownPeers()}), &al)
	if err != nil {
		fmt.Println("err::", err)
	} else {
		fmt.Println("connected", addr)
		s.addKnownPeer(addr, overlayNode)

		for _, peerNode := range al.List {
			go s.connectPeer(peerNode)
		}
	}
	cancel()

	return nil
}

func (s *Server) startConn() {
	state := node.ShardPublicOverlayID{Shard: -9223372036854775808, Workchain: 0, ZeroStateFileHash: ZeroStateFileHash}
	stateHash, _ := tl.Hash(state)

	keyHash, _ := tl.Hash(adnl.PublicKeyOverlay{
		Key: stateHash,
	})

	fmt.Println("overlay id ::", hex.EncodeToString(stateHash), hex.EncodeToString(keyHash))
	nodesList, _, _ := s.dht.FindOverlayNodes(context.Background(), stateHash)

	s.myself, _ = overlay.NewNode(stateHash, s.key)
	fmt.Println("initial peers ::", len(nodesList.List))

	for _, overlayNode := range nodesList.List {
		go func(peer overlay.Node) {
			err := s.connectPeer(peer)
			if err != nil {
				fmt.Println(err)
			}
		}(overlayNode)
	}
}
