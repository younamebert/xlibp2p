package p2p

import (
	"crypto/ecdsa"
	"errors"
	"github.com/younamebert/xlibp2p/discover"
	"github.com/younamebert/xlibp2p/log"
	"net"
	"sync"
	"time"
)

const (
	flagInbound  = 1
	flagOutbound = 1 << 1
	flagStatic   = 1 << 2
	flagDynamic  = 1 << 3
)

type Server interface {
	Bind(p Protocol)
	Start() error
	Stop()
}

// server manages all peer connections.
//
// The fields of Server are used as configuration parameters.
// You should set them before starting the Server. Fields may not be
// modified while the server is running.
type server struct {
	config  Config
	mu      sync.Mutex
	running bool
	//protocols contains the protocols supported by the server.
	//Matching protocols are launched for each peer.
	protocols []Protocol

	addpeer    chan *peerConn
	delpeer    chan Peer
	table      *discover.Table
	logger     log.Logger
	lastLookup time.Time
}

// Config Background network service configuration
type Config struct {
	ProtocolVersion uint8
	ListenAddr      string
	Key             *ecdsa.PrivateKey
	Discover        bool
	NodeDBPath      string
	StaticNodes     []*discover.Node
	BootstrapNodes  []*discover.Node
	MaxPeers        int
	Logger          log.Logger
}

// NewServer Creates background service object
func NewServer(config Config) Server {
	srv := &server{
		config: config,
		logger: config.Logger,
	}
	if config.Logger == nil {
		srv.logger = log.DefaultLogger()
	}
	return srv
}

// Bind network protocol function
func (srv *server) Bind(p Protocol) {
	if srv.protocols == nil {
		srv.protocols = make([]Protocol, 0)
	}
	// Add network protocol
	srv.protocols = append(srv.protocols, p)
}

// Stop background network function
func (srv *server) Stop() {

}

// Start start running the server.
func (srv *server) Start() error {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.running {
		return errors.New("server already running")
	}

	srv.running = true
	// Peer to peer session entity
	srv.addpeer = make(chan *peerConn)
	srv.delpeer = make(chan Peer)
	var err error
	// launch node discovery and UDP listener
	if srv.config.Discover {
		srv.table, err = discover.ListenUDP(srv.config.Key, srv.config.ListenAddr, srv.config.NodeDBPath)
		if err != nil {
			return err
		}
	}
	dynPeers := srv.config.MaxPeers / 2
	if !srv.config.Discover {
		dynPeers = 0
	}
	dialer := newDialState(srv.config.StaticNodes, srv.table, dynPeers)
	// launch TCP listener to accept connection
	if err = srv.listenAndServe(); err != nil {
		return err
	}

	go srv.run(dialer)
	srv.running = true
	return nil
}

func (srv *server) run(dialer *dialstate) {
	peers := make(map[discover.NodeId]Peer)
	tasks := make([]task, 0)
	pendingTasks := make([]task, 0)
	taskdone := make(chan task)
	delTask := func(t task) {
		for i := range tasks {
			if tasks[i] == t {
				tasks = append(tasks[:i], tasks[i+1:]...)
				break
			}
		}
	}

	scheduleTasks := func(new []task) {
		pt := append(pendingTasks, new...)
		start := 16 - len(tasks)
		if len(pt) < start {
			start = len(pt)
		}
		if start > 0 {
			tasks = append(tasks, pt[:start]...)
			for _, t := range pt[:start] {
				tt := t
				go func() {
					tt.Do(srv)
					taskdone <- tt
				}()
			}
			copy(pt, pt[start:])
			// pending tasks
			pendingTasks = pt[:len(pt)-start]
		}
	}
	for {
		now := time.Now()
		nt := dialer.newTasks(len(pendingTasks)+len(tasks), peers, now)
		// schedule tasks
		scheduleTasks(nt)
		select {
		// add peer
		case c := <-srv.addpeer:
			p := newPeer(c, srv.protocols)
			peers[c.id] = p
			srv.logger.Infof("save peer id to peers: %s", c.id)
			go srv.runPeer(p)
		// task is done
		case t := <-taskdone:
			dialer.taskDone(t, now)
			delTask(t)
		// delete peer
		case p := <-srv.delpeer:
			delete(peers, p.ID())
		}
	}
}

func (srv *server) runPeer(peer Peer) {
	peer.Run()
	srv.delpeer <- peer
}

func (srv *server) listenAndServe() error {
	addr := srv.config.ListenAddr
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		srv.logger.Errorf("p2p server listen and serve on %s err: %v", addr, err)
		return err
	}
	srv.logger.Infof("p2p server listen and serve on %s", addr)
	currentKey := srv.config.Key
	nId := discover.PubKey2NodeId(currentKey.PublicKey)
	//tcpAddr,_ := net.ResolveTCPAddr("", addr)
	//n := discover.NewNode(tcpAddr.IP, uint16(tcpAddr.Port), uint16(tcpAddr.Port),nId)
	srv.logger.Infof("p2p server node id: %s", nId)
	go srv.listenLoop(ln)
	return nil
}

// listenLoop runs in its own goroutine and accepts
// request of connections.
func (srv *server) listenLoop(ln net.Listener) {
	defer func() {
		if err := ln.Close(); err != nil {
			srv.logger.Errorln(err)
		}
	}()
	for {
		rw, err := ln.Accept()
		if err != nil {
			srv.logger.Errorf("p2p listenner accept err %v", err)
			return
		}
		c := srv.newPeerConn(rw, flagInbound, nil)
		go c.serve()
	}
}

func (srv *server) newPeerConn(rw net.Conn, flag int, dst *discover.NodeId) *peerConn {
	pubKey := srv.config.Key.PublicKey
	mId := discover.PubKey2NodeId(pubKey)
	c := &peerConn{
		logger:  srv.logger,
		self:    mId,
		flag:    flag,
		server:  srv,
		key:     srv.config.Key,
		rw:      rw,
		version: srv.config.ProtocolVersion,
	}
	if dst != nil {
		c.id = *dst
	}
	return c
}
