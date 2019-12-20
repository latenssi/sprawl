package p2p

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/sprawl/sprawl/interfaces"

	libp2p "github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	discovery "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	libp2pConfig "github.com/libp2p/go-libp2p/config"
	"github.com/sprawl/sprawl/errors"
	"github.com/sprawl/sprawl/pb"
)

const networkID = "/sprawl/"

// P2p stores all things required to converse with other peers in the Sprawl network and save data locally
type P2p struct {
	Logger           interfaces.Logger
	Config           interfaces.Config
	privateKey       crypto.PrivKey
	publicKey        crypto.PubKey
	ps               *pubsub.PubSub
	ctx              context.Context
	host             host.Host
	kademliaDHT      *dht.IpfsDHT
	routingDiscovery *discovery.RoutingDiscovery
	peerChan         <-chan peer.AddrInfo
	input            chan pb.WireMessage
	subscriptions    map[string]chan bool
	Receiver         interfaces.Receiver
}

// NewP2p returns a P2p struct with an input channel
func NewP2p(log interfaces.Logger, config interfaces.Config, privateKey crypto.PrivKey, publicKey crypto.PubKey) (p2p *P2p) {
	p2p = &P2p{
		Logger:        log,
		Config:        config,
		privateKey:    privateKey,
		publicKey:     publicKey,
		input:         make(chan pb.WireMessage),
		subscriptions: make(map[string]chan bool),
	}
	return
}

// AddReceiver registers a data receiver function with p2p
func (p2p *P2p) AddReceiver(receiver interfaces.Receiver) {
	p2p.Receiver = receiver
}

func (p2p *P2p) initContext() {
	p2p.ctx = context.Background()
}

func (p2p *P2p) initHost(options ...libp2pConfig.Option) {
	var err error
	p2p.host, err = libp2p.New(
		p2p.ctx,
		options...)
	if !errors.IsEmpty(err) {
		if p2p.Logger != nil {
			p2p.Logger.Error(errors.E(errors.Op("Creating host"), err))
		}
	}
}

func (p2p *P2p) initPubSub() {
	var err error
	p2p.ps, err = pubsub.NewGossipSub(p2p.ctx, p2p.host)
	if !errors.IsEmpty(err) {
		if p2p.Logger != nil {
			p2p.Logger.Error(err)
		}
	}
}

func (p2p *P2p) bootstrapDHT() {
	var err error

	bootstrapConfig := dht.BootstrapConfig{
		Queries: 1,
		Period:  time.Duration(2 * time.Minute),
		Timeout: time.Duration(10 * time.Second),
	}

	err = p2p.kademliaDHT.BootstrapWithConfig(p2p.ctx, bootstrapConfig)

	if !errors.IsEmpty(err) {
		if p2p.Logger != nil {
			p2p.Logger.Error(errors.E(errors.Op("Bootstrap with config"), err))
		}
	}
}

func (p2p *P2p) bootstrapNetwork() {
	var wg sync.WaitGroup
	if p2p.Logger != nil {
		p2p.Logger.Info("Connecting to bootstrap peers")
	}
	for _, peerAddr := range defaultBootstrapPeers() {
		peerinfo, err := peer.AddrInfoFromP2pAddr(peerAddr)
		if err != nil && p2p.Logger != nil {
			p2p.Logger.Errorf("Invalid bootstrap multiaddress: %s", err)
		} else {
			wg.Add(1)

			go func() {
				defer wg.Done()
				if err := p2p.host.Connect(p2p.ctx, *peerinfo); !errors.IsEmpty(err) {
					if p2p.Logger != nil {
						p2p.Logger.Debugf("Error connecting to bootstrap peer %s", err)
					} else {
						p2p.Logger.Debugf("Successfully connected to bootstrap peer %s", peerinfo)
					}
				}
			}()
		}
	}

	wg.Wait()
}

func (p2p *P2p) startDiscovery() {
	// Add Kademlia routing discovery
	p2p.routingDiscovery = discovery.NewRoutingDiscovery(p2p.kademliaDHT)

	// Start the advertiser service
	discovery.Advertise(p2p.ctx, p2p.routingDiscovery, networkID)

	var err error
	// Ingest newly found peers into p2p.peerChan
	p2p.peerChan, err = p2p.routingDiscovery.FindPeers(p2p.ctx, networkID)

	if !errors.IsEmpty(err) {
		if p2p.Logger != nil {
			p2p.Logger.Error(errors.E(errors.Op("Find peers"), err))
		}
	}
}

func (p2p *P2p) connectToPeers() {
	if p2p.Logger != nil {
		p2p.Logger.Infof("This node's ID: %s\n", p2p.host.ID())
		p2p.Logger.Infof("Listening to the following addresses: %s\n", p2p.host.Addrs())
	}
	var wg sync.WaitGroup
	go func(ctx context.Context) {
		for peer := range p2p.peerChan {
			if peer.ID == p2p.host.ID() {
				if p2p.Logger != nil {
					p2p.Logger.Debug("Found yourself!")
				}
				continue
			}
			if p2p.Logger != nil {
				p2p.Logger.Infof("Found a new peer: %s\n", peer.ID)
			}

			// Waits on each peerInfo until they are connected or the connection failed
			wg.Add(1)
			go func(ctx context.Context) {
				defer wg.Done()
				if err := p2p.host.Connect(ctx, peer); !errors.IsEmpty(err) {
					if p2p.Logger != nil {
						p2p.Logger.Error(errors.E(errors.Op("Connect"), err))
					}
				} else {
					if p2p.Logger != nil {
						p2p.Logger.Infof("Connected to: %s\n", peer)
					}
				}
			}(p2p.ctx)
			wg.Wait()
		}
	}(p2p.ctx)
}

func (p2p *P2p) handleInput(message *pb.WireMessage) {
	buf, err := proto.Marshal(message)
	if !errors.IsEmpty(err) {
		if p2p.Logger != nil {
			p2p.Logger.Error(errors.E(errors.Op("Marshal proto"), err))
		}
	}
	err = p2p.ps.Publish(string(message.GetChannelID()), buf)
	if !errors.IsEmpty(err) {
		if p2p.Logger != nil {
			p2p.Logger.Error(errors.E(errors.Op("Marshal proto"), fmt.Sprintf("%v, message data: %s", err.Error(), message.Data)))
		}
	}
}

func (p2p *P2p) listenForInput() (err error) {
	for {
		select {
		case message := <-p2p.input:
			p2p.handleInput(&message)
		}
	}
}

// Send queues a message for sending to other peers
func (p2p *P2p) Send(message *pb.WireMessage) {
	if p2p.Logger != nil {
		p2p.Logger.Debugf("Sending order %s to channel %s", message.GetData(), message.GetChannelID())
	}
	go func(ctx context.Context) {
		p2p.input <- *message
	}(p2p.ctx)
}

// Subscribe subscribes to a libp2p pubsub channel defined with "channel"
func (p2p *P2p) Subscribe(channel *pb.Channel) {
	if p2p.Logger != nil {
		p2p.Logger.Infof("Subscribing to channel %s with options: %s", channel.GetId(), channel.GetOptions())
	}
	sub, err := p2p.ps.Subscribe(string(channel.GetId()))
	if !errors.IsEmpty(err) {
		if p2p.Logger != nil {
			p2p.Logger.Error(errors.E(errors.Op("Subscribe"), err))
		}
	}

	quitSignal := make(chan bool)
	p2p.subscriptions[string(channel.GetId())] = quitSignal

	go func(ctx context.Context) {
		for {
			msg, err := sub.Next(ctx)
			if !errors.IsEmpty(err) {
				if p2p.Logger != nil {
					p2p.Logger.Error(errors.E(errors.Op("Next Message"), err))
				}
			}

			data := msg.GetData()
			peer := msg.GetFrom()

			if peer != p2p.host.ID() {
				if p2p.Logger != nil {
					p2p.Logger.Infof("Received data from peer %s: %s", peer, data)
				}

				if p2p.Receiver != nil {
					err = p2p.Receiver.Receive(data)
					if !errors.IsEmpty(err) {
						if p2p.Logger != nil {
							p2p.Logger.Error(errors.E(errors.Op("Receive data"), err))
						}
					}
				} else {
					if p2p.Logger != nil {
						p2p.Logger.Warn("Receiver not registered with p2p, not parsing any incoming data!")
					}
				}
			}

			select {
			case quit := <-quitSignal: //Delete subscription
				if quit {
					delete(p2p.subscriptions, string(channel.GetId()))
					return
				}
			default:
			}
		}
	}(p2p.ctx)
}

// Unsubscribe sends a quit signal to a channel goroutine
func (p2p *P2p) Unsubscribe(channel *pb.Channel) {
	p2p.subscriptions[string(channel.GetId())] <- true
}

// Run runs the p2p network
func (p2p *P2p) Run() {
	p2p.initContext()

	// Initialize the p2p host with options
	p2p.initHost(p2p.CreateOptions()...)

	// Create local Kademlia DHT routing table
	p2p.bootstrapDHT()

	// Connect to Sprawl & IPFS main nodes for peer discovery
	p2p.bootstrapNetwork()

	// Start finding peers on the network
	p2p.startDiscovery()

	// Start PubSub
	p2p.initPubSub()

	// Listen for local and network input
	go func() {
		p2p.listenForInput()
	}()

	// Continuously connect to other Sprawl peers
	p2p.connectToPeers()
}

// Close closes the underlying libp2p host
func (p2p *P2p) Close() {
	p2p.Logger.Debug("P2P shutting down")
	p2p.host.Close()
}
