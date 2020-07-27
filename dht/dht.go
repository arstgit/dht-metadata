// Package dht implementions. It maintain neighbor nodes and keep farming.
package dht

import (
	"fmt"
	"log"
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
	nodes  []*node
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
	sendFindNodeRandomTicker := time.NewTicker(3 * time.Second)
	cleanUnhealthyNodesTicker := time.NewTicker(5 * time.Second)
	epollTimeout := 500

	for {
		select {
		case <-sendFindNodeRandomTicker.C:
			dht.SendFindNodeRandom()
		case <-cleanUnhealthyNodesTicker.C:
			dht.cleanNodes()
		default:
			dht.waitEpoll(epollTimeout)
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
	for _, node := range dht.nodes {
		if id == node.id || addr == node.address {
			return
		}
	}
	dht.nodes = append(dht.nodes, &node{id: id, fd: -1, address: addr, lastActive: time.Now(), status: newAdded})
}

func (dht *Dht) cleanNodes() {
	if len(dht.nodes) < 20 {
		return
	}

	// delete unhealthy nodes
	for i, node := range dht.nodes {
		if node.status == unhealthy {
			dht.removeNode(i)
		}
	}

	// keep nodes num below maxNodeNum
	for {
		if len(dht.nodes) >= maxNodeNum {
			i, _ := dht.getOldestNode()

			dht.removeNode(i)
		} else {
			break
		}
	}

}

func (dht *Dht) removeNode(i int) {
	node := dht.nodes[i]
	if node.fd != -1 {
		if node.status != waitForPacket {
			log.Fatal("node status not waitforpacket")
		}
		err := syscall.Close(node.fd)
		if err != nil {
			log.Fatal("close ", err)
		}
	}

	dht.nodes[i] = dht.nodes[len(dht.nodes)-1]
	dht.nodes = dht.nodes[:len(dht.nodes)-1]
}

func (dht *Dht) getNode(flags nodeStatus) (int, *node) {
	for i, node := range dht.nodes {
		if (node.status & flags) == node.status {
			return i, dht.nodes[i]
		}
	}

	return -1, nil
}

func (dht *Dht) getOldestNode() (int, *node) {
	if len(dht.nodes) == 0 {
		log.Fatal("node num == 0")
	}

	minLastActiveNode := dht.nodes[0]

	i := 0
	for _, node := range dht.nodes {
		if minLastActiveNode.lastActive.After(node.lastActive) {
			minLastActiveNode = node
			i++
		}
	}

	return i, minLastActiveNode
}

func (dht *Dht) findNodeByFd(fd int) *node {
	if !(fd > 2) {
		log.Fatal("findNodeByFd, invalid fd")
	}

	for _, node := range dht.nodes {
		if node.fd == fd {
			return node
		}
	}

	return nil
}
