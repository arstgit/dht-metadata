// Package dht implementions. It maintain neighbor nodes and keep farming.
package dht

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"
	"unsafe"
	"syscall"

	"github.com/anacrolix/torrent/bencode"
)

const bootstrap1 = "router.utorrent.com:6881"

type nodeid [20]byte

type dhtStatus int

const (
	running dhtStatus = iota
	suspended
)

type nodeStatus int

const (
	live nodeStatus = iota
	dead
)

type node struct {
	id         nodeid
	address    string
	lastActive time.Time
	status     nodeStatus
	listener net.PacketConn
}

// Dht maintain running status.
type Dht struct {
	efd int
	selfid   nodeid
	status   dhtStatus
	nodes    []node
}

type findNode struct {
	T string `json:"t"`
	y string
	q string
	a struct {
		id     string
		target string
	}
}

// New Create a listening socket, allocate spaces for storing nodes and dht status.
func New() *Dht {
	dht := new(Dht)

	dht.selfid = generateRandNodeid()
	dht.nodes = append(dht.nodes, node{address: bootstrap1, lastActive: time.Now(), status: live})

	efd, err := syscall.EpollCreate1(0)
	if err != nil {
		log.Fatal("epoll_create1()")
	}
	dht.efd = efd

	dht.status = suspended

	return dht
}

// FreeDht free allocated space.
func (dht *Dht) FreeDht() {
	syscall.Close(dht.efd)

	for _, node := range dht.nodes {
		err := node.listener.Close()
		if err != nil {
			log.Fatal(err)
		}
	}

	return
}

// YieldStatus return dht running status as a string.
func (dht *Dht) YieldStatus() string {
	s := fmt.Sprintf("%#v", dht)

	return s
}

// FarmStep makes the farming node action proceeds.
func (dht *Dht) FarmStep() {

}

// FindNodeRandom sent find_node query to random node for random target.
func (dht *Dht) FindNodeRandom() {
	//bencode.Decoder()


	tid := generateTid()
	msg := findNode{T: tid, y: "q", q: "find_node", }
	payload := bencode.MustMarshal(msg)
	log.Printf("marshal: %x", payload)
	targetNode := dht.getRandomNode()

	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	dst, err := net.ResolveUDPAddr("udp", targetNode.address)
	if err != nil {
		log.Fatal(err)
	}

	conn.WriteTo(payload, dst)
	if err != nil {
		log.Fatal(err)
	}

	targetNode.listener = conn

	fd := conn
}

func (dht *Dht) removeNode(i int) {
	dht.nodes[i] = dht.nodes[len(dht.nodes)-1]
	dht.nodes = dht.nodes[:len(dht.nodes)-1]
}

func (dht *Dht) getRandomNode() *node {
	i := rand.Intn(len(dht.nodes))

	return &dht.nodes[i]
}

func generateTid() string {
	s := fmt.Sprintf("%s", string(rand.Intn(256)))

	return s
}

func generateRandNodeid() nodeid {
	var id nodeid
	r := rand.Uint32()
	for i := 0; i < 5; i++ {
		copy(id[i*4:], (*[4]byte)(unsafe.Pointer(&r))[:])
	}

	log.Printf("generate nodeid: %x", id)

	return id
}
