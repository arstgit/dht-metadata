// Package dht implementions. It maintain neighbor nodes and keep farming.
package dht

import (
	"fmt"
	"log"
	"math/rand"
	"syscall"
	"time"
)

const maxNodeNum = 200

const (
	// EPOLLET uint32
	EPOLLET = 1 << 31
	// MaxEpollEvents num
	MaxEpollEvents = 32
)

// router.utorrent.com
const bootstrap1 = "82.221.103.244:6881"

type dhtStatus int

const (
	running dhtStatus = iota
	suspended
)

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

func (dht *Dht) waitEpoll(timeout int) {
	var events [MaxEpollEvents]syscall.EpollEvent

	nevents, err := syscall.EpollWait(dht.efd, events[:], timeout)
	if err != nil {
		if err != syscall.EINTR {
			log.Fatal("epoll_wait ", err)
		}
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
		if id == node.id || addr == node.address {
			return
		}
	}
	dht.nodes = append(dht.nodes, node{id: id, fd: -1, address: addr, lastActive: time.Now(), status: newAdded})
}

func (dht *Dht) removeNode(i int) {
	dht.nodes[i] = dht.nodes[len(dht.nodes)-1]
	dht.nodes = dht.nodes[:len(dht.nodes)-1]
}

func (dht *Dht) getRandomNode() *node {
	i := rand.Intn(len(dht.nodes))

	return &dht.nodes[i]
}
