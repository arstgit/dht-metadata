// Package dht implementions. It maintain neighbor nodes and keep farming.
package dht

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const maxNodeNum = 1000

const (
	// EPOLLET uint32
	EPOLLET = 1 << 31
	// MaxEpollEvents num
	MaxEpollEvents = 32
)

// router.utorrent.com
const bootstrap1 = "82.221.103.244:6881"

type nodeid [20]byte

func (id nodeid) toString() string {
	var tmp = [20]byte(id)

	return string(tmp[:])
}

type dhtStatus int

const (
	running dhtStatus = iota
	suspended
)

type nodeStatus int
type compactAddr [6]byte

const (
	idle nodeStatus = iota
	waitForPacket
	dead
)

type node struct {
	id         nodeid
	address    compactAddr
	lastActive time.Time
	status     nodeStatus
	fd         int // -1 means not allocated yet
	sentType   string
	tid        string
}

// Dht maintain running status.
type Dht struct {
	efd    int
	selfid nodeid
	status dhtStatus
	nodes  []node
}

// New Create a listening socket, allocate spaces for storing nodes and dht status.
func New() *Dht {
	dht := new(Dht)

	dht.selfid = generateRandNodeid()

	// push bootstrap node into dht.
	dht.addNode(convertToCompactAddr(bootstrap1), generateRandNodeid())

	efd, err := syscall.EpollCreate1(0)
	if err != nil {
		log.Fatal("epoll_create1()")
	}
	dht.efd = efd

	dht.status = suspended

	return dht
}

func convertToCompactAddr(s string) compactAddr {
	var addr compactAddr

	arr := strings.Split(s, ":")
	ip := arr[0]
	port := arr[1]

	ip1 := binary.LittleEndian.Uint32(net.ParseIP(ip).To4())
	port1, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		log.Fatal(err)
	}

	ip2 := make([]byte, 4)
	port2 := make([]byte, 2)

	binary.BigEndian.PutUint32(ip2, uint32(ip1))
	binary.BigEndian.PutUint16(port2, uint16(port1))

	copy(addr[:], append(ip2, port2...))
	return addr
}

// FreeDht free allocated space.
func (dht *Dht) FreeDht() {
	syscall.Close(dht.efd)

	for _, node := range dht.nodes {
		err := syscall.Close(node.fd)
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

// Run send packets while listening active node port.
func (dht *Dht) Run() {
	ticker := time.NewTicker(3 * time.Second)

	for {
		select {
		case <-ticker.C:
			log.Print("tic, SendFindNodeRandom")
			dht.SendFindNodeRandom()
		default:
			dht.waitEpoll(1000)
		}
	}

	// ticker.Stop()
}
func (dht *Dht) waitEpoll(timeout int) {
	var events [MaxEpollEvents]syscall.EpollEvent

	nevents, err := syscall.EpollWait(dht.efd, events[:], timeout)
	if err != nil {
		log.Printf("epoll_wait %#v", err)
		time.Sleep(1000 * time.Second)
		log.Fatal("epoll_wait ", err)
	}

	for i := 0; i < nevents; i++ {
		fd := int(events[i].Fd)

		buf := make([]byte, 1472)
		n, fromAddr, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			log.Fatal(err)
		}
		dht.processRecv(fd, buf, n, fromAddr)
	}
}

func (dht *Dht) addNode(addr compactAddr, id nodeid) {
	n := len(dht.nodes)

	log.Print("dht.node num: ", n)
	if n >= maxNodeNum {
		return
	}

	for _, node := range dht.nodes {
		if id == node.id {
			return
		}
	}
	dht.nodes = append(dht.nodes, node{id: id, fd: -1, address: addr, lastActive: time.Now(), status: idle})
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

	return id
}
