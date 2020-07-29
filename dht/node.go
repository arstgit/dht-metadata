package dht

import (
	"fmt"
	"log"
	"math/rand"
	"syscall"
	"time"
)

type nodeid [20]byte

func (id nodeid) toString() string {
	var tmp = [20]byte(id)

	return string(tmp[:])
}

type nodeStatus int

const (
	newAdded nodeStatus = 1 << iota
	waitForPacket
	receivedPacket
	unhealthy
)

type node struct {
	id         nodeid
	address    compactAddr
	lastActive time.Time
	lastQuery  time.Time
	status     nodeStatus
	fd         int // -1 means not allocated yet. Do not dup it because of epoll interest list.
	sentType   string
	tid        string
	priv       *job
}

func (node *node) setStatus(status nodeStatus) {
	switch status {
	case waitForPacket:
		node.lastQuery = time.Now()
		node.lastActive = time.Now()
	case receivedPacket:
		err := syscall.Close(node.fd)
		if err != nil {
			log.Fatal("close ", err)
		}

		node.fd = -1
		node.lastActive = time.Now()
	case unhealthy:
		err := syscall.Close(node.fd)
		if err != nil {
			log.Fatal("close ", err)
		}

		node.fd = -1
		node.lastActive = time.Now()
	default:
		log.Panic("wrong status ", status)
	}

	node.status = status
}

func (node *node) onEvent(fd int, events int) {
	// we expect epollin only
	if !((events & syscall.EPOLLIN) == events) {
		log.Panic("not only epollin")
	}

	buf := make([]byte, 1472)
	_, _, err := syscall.Recvfrom(fd, buf, 0)
	if err != nil {
		log.Fatal(err)
	}

	if node == nil {
		log.Fatalf("node is nil, %d", fd)
	}
	if node.status != waitForPacket {
		log.Fatal("node status not waitforpacket")
	}

	log.Printf("received package type %s", node.sentType)

	switch node.sentType {
	case "find_node":
		err := node.priv.processFindNodeRes(node, buf)
		if err != nil {
			log.Print("processfindnoderes ", err)
		}
	case "get_peers":
		err := node.priv.processGetPeersRes(node, buf)
		if err != nil {
			log.Print("processgetpeersres ", err)
		}
	default:
		log.Fatal("node.sentType ", node.sentType)
	}
}
func (node *node) checkTid(tid string) error {
	if node.tid == tid {
		return nil
	}

	return fmt.Errorf("tid not equal")

}

func (node *node) allocateFd(efd int) {
	fd := node.fd
	if fd == -1 {
		fd = allocateSocket(syscall.SOCK_DGRAM, true)
	}
	node.fd = fd

	var event syscall.EpollEvent

	event.Events = syscall.EPOLLIN | EPOLLET
	event.Fd = int32(fd)
	if err := syscall.EpollCtl(efd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
		log.Fatal("epoll_ctl_add ", err)
	}
}

func (node *node) generateTid() {
	s := fmt.Sprintf("%s", string(rand.Intn(255)))
	node.tid = s
}
