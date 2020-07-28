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
