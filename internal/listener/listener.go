package listener

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/adnl/node"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	rldphttp "github.com/xssnick/tonutils-go/adnl/rldp/http"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	reflect "reflect"
	"sync"
	"time"
	"tonmempool/config"
)

type ExternalMessageHandler = func(message tlb.ExternalMessage) error
type Opt = func(s *Listener)

type Listener struct {
	mtx *sync.Mutex

	key  ed25519.PrivateKey
	dht  *dht.Client
	gate *adnl.Gateway

	overlayId  []byte
	knownPeers map[string]overlay.Node

	myself *overlay.Node
	peers  chan overlay.Node

	onExternalMessageHandler ExternalMessageHandler
}

func WithOnExternalMessage(handler ExternalMessageHandler) Opt {
	return func(s *Listener) {
		s.onExternalMessageHandler = handler
	}
}

func NewServer(
	dhtClient *dht.Client,
	gateway *adnl.Gateway,
	key ed25519.PrivateKey,
	opts ...Opt,
) *Listener {
	state := node.ShardPublicOverlayID{
		Shard:             -9223372036854775808,
		Workchain:         0,
		ZeroStateFileHash: config.ZeroStateFileHash,
	}

	stateHash, _ := tl.Hash(state)
	overlayId, _ := tl.Hash(adnl.PublicKeyOverlay{Key: stateHash})

	listener := &Listener{
		mtx:        &sync.Mutex{},
		dht:        dhtClient,
		gate:       gateway,
		key:        key,
		knownPeers: make(map[string]overlay.Node),
		overlayId:  overlayId,
		peers:      make(chan overlay.Node),
		onExternalMessageHandler: func(message tlb.ExternalMessage) error {
			fmt.Println("external", message)

			return nil
		},
	}
	listener.myself, _ = overlay.NewNode(stateHash, key)

	for _, opt := range opts {
		opt(listener)
	}

	return listener
}

func (s *Listener) onOverlayCustomMessage(msg *adnl.MessageCustom) error {
	switch v := msg.Data.(type) {
	case overlay.Broadcast:
		var gh any
		_, _ = tl.Parse(&gh, v.Data, true)

		switch overlayMsg := gh.(type) {
		case node.NewExternalMessageBroadcast:
			var extMsg tlb.ExternalMessage

			extCell, err := cell.FromBOC(overlayMsg.Message.Data)
			if err != nil {
				return errors.Wrap(err, "load ext msg from data")
			}

			if err := tlb.LoadFromCell(&extMsg, extCell.BeginParse()); err != nil {
				return errors.Wrap(err, "ext msg load cell")
			}

			return s.onExternalMessageHandler(extMsg)
		default:
			return nil
		}
	default:
		return nil
	}

	return nil
}

func (s *Listener) nodesConnector() {
	for peer := range s.peers {
		s.tryConnectPeer(peer)
	}
}

func (s *Listener) pingNodes() {
	log.Info().Msg("start ping nodes")

	for {
		if len(s.knownPeers) >= 30 {
			time.Sleep(time.Second * 3)
			continue
		}

		wg := sync.WaitGroup{}
		for _, overlayNode := range s.knownPeers {
			wg.Add(1)

			go func(node overlay.Node) {
				defer wg.Done()
				if err := s.tryConnectPeer(node); err != nil {
					log.Error().Err(err).Msg("cannot connect peer")
				}
			}(overlayNode)
		}

		wg.Wait()

		time.Sleep(time.Second)
	}
}

func (s *Listener) tryConnectPeer(overlayNode overlay.Node) error {
	nodeId, _ := tl.Hash(overlayNode.ID)
	addrs, keyN, err := s.dht.FindAddresses(context.Background(), nodeId)
	if err != nil {
		return err
	}
	if len(addrs.Addresses) == 0 {
		return nil
	}

	addr := addrs.Addresses[0].IP.String() + ":" + fmt.Sprint(addrs.Addresses[0].Port)

	ax, err := s.gate.RegisterClient(addr, keyN)
	if err != nil {
		return err
	}

	var al overlay.NodesList
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	err = ax.Query(ctx, overlay.WrapQuery(overlayNode.Overlay, &overlay.GetRandomPeers{List: s.getKnownPeers()}), &al)
	if err != nil {
		return err
	}

	s.mtx.Lock()
	s.knownPeers[hex.EncodeToString(ax.GetID())] = overlayNode
	s.mtx.Unlock()

	for _, peerNode := range al.List {
		go func(p overlay.Node) {
			s.tryConnectPeer(p)
		}(peerNode)
	}

	return nil
}

func (s *Listener) LogKnownPeers() {
	for {
		fmt.Println("known_peers", len(s.knownPeers))
		time.Sleep(time.Second * 3)
	}
}

func (s *Listener) getKnownPeers() overlay.NodesList {
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

func (s *Listener) StartSearchingPeers() {
	state := node.ShardPublicOverlayID{Shard: -9223372036854775808, Workchain: 0, ZeroStateFileHash: config.ZeroStateFileHash}
	stateHash, _ := tl.Hash(state)

	keyHash, _ := tl.Hash(adnl.PublicKeyOverlay{Key: stateHash})
	nodesList, _, _ := s.dht.FindOverlayNodes(context.Background(), stateHash)

	log.Info().
		Str("overlay_id", hex.EncodeToString(keyHash)).
		Int("initial_nodes", len(nodesList.List)).
		Msg("start searching nodes")

	go s.nodesConnector()
	go s.pingNodes()

	wg := sync.WaitGroup{}
	for _, overlayNode := range nodesList.List {
		wg.Add(1)
		go func(peer overlay.Node) {
			defer wg.Done()

			err := s.tryConnectPeer(peer)
			if err != nil {
				return
			}

		}(overlayNode)
	}

	wg.Wait()

	log.Info().Int("nodes", len(s.knownPeers)).Msg("connected initial peers")
}

func (s *Listener) UpdateDHTRecords(ctx context.Context) error {
	addr := s.gate.GetAddressList()

	ctxStore, cancel := context.WithTimeout(ctx, 90*time.Second)
	stored, id, err := s.dht.StoreAddress(ctxStore, addr, 10*time.Minute, s.key, 8)
	cancel()
	if err != nil && stored == 0 {
		return err
	}

	_, _, err = s.dht.FindAddresses(ctx, id)
	if err != nil {
		return err
	}

	log.Info().Int("nodes_count", stored).Msg("node address updated on")

	return nil
}

func (s *Listener) InitHandlers() {
	fmt.Println("overlay id", hex.EncodeToString(s.overlayId))

	s.gate.SetConnectionHandler(func(client adnl.Peer) error {
		adnlAddr, err := rldphttp.SerializeADNLAddress(client.GetID())
		if err != nil {
			return err
		}

		fmt.Println("gate connect", adnlAddr)

		adnlPeer := overlay.CreateExtendedADNL(client)
		ovr := adnlPeer.WithOverlay(s.overlayId)
		ovr.SetBroadcastHandler(func(msg tl.Serializable, trusted bool) error {
			fmt.Println("broadcast handler", trusted, msg)

			return nil
		})

		ovr.SetCustomMessageHandler(func(msg *adnl.MessageCustom) error {
			switch v := msg.Data.(type) {
			case overlay.Broadcast:
				var gh any
				_, _ = tl.Parse(&gh, v.Data, true)

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

					return s.onExternalMessageHandler(extMsg)
				default:
					return nil
				}

				fmt.Println(reflect.TypeOf(gh))
			default:
				fmt.Println("default handler", reflect.TypeOf(v))
				return nil
			}

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
}
