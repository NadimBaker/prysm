package p2p

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	multiaddr "github.com/multiformats/go-multiaddr"
	testDB "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

type mockListener struct{}

func (mockListener) Self() *enode.Node {
	panic("implement me")
}

func (mockListener) Close() {
	//no-op
}

func (mockListener) Lookup(enode.ID) []*enode.Node {
	panic("implement me")
}

func (mockListener) ReadRandomNodes([]*enode.Node) int {
	panic("implement me")
}

func (mockListener) Resolve(*enode.Node) *enode.Node {
	panic("implement me")
}

func (mockListener) LookupRandom() []*enode.Node {
	panic("implement me")
}

func (mockListener) Ping(*enode.Node) error {
	panic("implement me")
}

func (mockListener) RequestENR(*enode.Node) (*enode.Node, error) {
	panic("implement me")
}

func (mockListener) LocalNode() *enode.LocalNode {
	panic("implement me")
}

func createHost(t *testing.T, port int) (host.Host, *ecdsa.PrivateKey, net.IP) {
	ipAddr, pkey := createAddrAndPrivKey(t)
	ipAddr = net.ParseIP("127.0.0.1")
	listen, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", ipAddr, port))
	if err != nil {
		t.Fatalf("Failed to p2p listen: %v", err)
	}
	h, err := libp2p.New(context.Background(), []libp2p.Option{privKeyOption(pkey), libp2p.ListenAddrs(listen)}...)
	if err != nil {
		t.Fatal(err)
	}
	return h, pkey, ipAddr
}

func TestService_Stop_SetsStartedToFalse(t *testing.T) {
	s, _ := NewService(&Config{})
	s.started = true
	s.dv5Listener = &mockListener{}
	_ = s.Stop()

	if s.started != false {
		t.Error("Expected Service.started to be false, got true")
	}
}

func TestService_Stop_DontPanicIfDv5ListenerIsNotInited(t *testing.T) {
	s, _ := NewService(&Config{})
	_ = s.Stop()
}

func TestService_Start_OnlyStartsOnce(t *testing.T) {
	db := testDB.SetupDB(t)
	defer testDB.TeardownDB(t, db)
	hook := logTest.NewGlobal()

	cfg := &Config{
		TCPPort:  2000,
		UDPPort:  2000,
		Encoding: "ssz",
		BeaconDB: db,
	}
	s, err := NewService(cfg)
	s.dv5Listener = &mockListener{}
	s.genesisValidatorsRoot = make([]byte, 32)
	s.genesisTime = time.Now()
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	if s.started != true {
		t.Error("Expected service to be started")
	}
	s.Start()
	testutil.AssertLogsContain(t, hook, "Attempted to start p2p service when it was already started")
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestService_Status_NotRunning(t *testing.T) {
	s := &Service{started: false}
	s.dv5Listener = &mockListener{}
	if s.Status().Error() != "not running" {
		t.Errorf("Status returned wrong error, got %v", s.Status())
	}
}

func TestListenForNewNodes(t *testing.T) {
	// Setup bootnode.
	db := testDB.SetupDB(t)
	defer testDB.TeardownDB(t, db)
	cfg := &Config{}
	port := 2000
	cfg.UDPPort = uint(port)
	_, pkey := createAddrAndPrivKey(t)
	ipAddr := net.ParseIP("127.0.0.1")
	genesisTime := time.Now()
	genesisValidatorsRoot := make([]byte, 32)
	s := &Service{
		cfg:                   cfg,
		genesisTime:           genesisTime,
		genesisValidatorsRoot: genesisValidatorsRoot,
	}
	bootListener := s.createListener(ipAddr, pkey)
	defer bootListener.Close()

	// Use shorter period for testing.
	currentPeriod := pollingPeriod
	pollingPeriod = 1 * time.Second
	defer func() {
		pollingPeriod = currentPeriod
	}()

	bootNode := bootListener.Self()

	var listeners []*discover.UDPv5
	var hosts []host.Host
	// setup other nodes.
	cfg = &Config{
		BootstrapNodeAddr:   []string{bootNode.String()},
		Discv5BootStrapAddr: []string{bootNode.String()},
		Encoding:            "ssz",
		MaxPeers:            30,
	}
	for i := 1; i <= 5; i++ {
		h, pkey, ipAddr := createHost(t, port+i)
		cfg.UDPPort = uint(port + i)
		cfg.TCPPort = uint(port + i)
		s := &Service{
			cfg:                   cfg,
			genesisTime:           genesisTime,
			genesisValidatorsRoot: genesisValidatorsRoot,
		}
		listener, err := s.startDiscoveryV5(ipAddr, pkey)
		if err != nil {
			t.Errorf("Could not start discovery for node: %v", err)
		}
		listeners = append(listeners, listener)
		hosts = append(hosts, h)
	}

	// close peers upon exit of test
	defer func() {
		for _, h := range hosts {
			_ = h.Close()
		}
	}()

	cfg.UDPPort = 14000
	cfg.TCPPort = 14001
	cfg.BeaconDB = db
	s, err := NewService(cfg)
	s.genesisValidatorsRoot = genesisValidatorsRoot
	s.genesisTime = genesisTime
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	time.Sleep(2 * time.Second)
	peers := s.host.Network().Peers()
	if len(peers) != 5 {
		t.Errorf("Not all peers added to peerstore, wanted %d but got %d", 5, len(peers))
	}

	// close down all peers
	for _, listener := range listeners {
		listener.Close()
	}

	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestPeer_Disconnect(t *testing.T) {
	h1, _, _ := createHost(t, 5000)
	defer h1.Close()

	s := &Service{
		host: h1,
	}

	h2, _, ipaddr := createHost(t, 5001)
	defer h2.Close()

	h2Addr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", ipaddr, 5001, h2.ID()))
	if err != nil {
		t.Fatal(err)
	}
	addrInfo, err := peer.AddrInfoFromP2pAddr(h2Addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.host.Connect(context.Background(), *addrInfo); err != nil {
		t.Fatal(err)
	}
	if len(s.host.Network().Peers()) != 1 {
		t.Fatalf("Number of peers is %d when it was supposed to be %d", len(s.host.Network().Peers()), 1)
	}
	if len(s.host.Network().Conns()) != 1 {
		t.Fatalf("Number of connections is %d when it was supposed to be %d", len(s.host.Network().Conns()), 1)
	}
	if err := s.Disconnect(h2.ID()); err != nil {
		t.Fatal(err)
	}
	if len(s.host.Network().Conns()) != 0 {
		t.Fatalf("Number of connections is %d when it was supposed to be %d", len(s.host.Network().Conns()), 0)
	}
}
