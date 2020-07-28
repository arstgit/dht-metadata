package dht

import (
	"fmt"
	"log"
	"math/rand"
	"net"
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
		fd = allocateDgramSocket(efd)
	}
	node.fd = fd

	var event syscall.EpollEvent

	event.Events = syscall.EPOLLIN | EPOLLET
	event.Fd = int32(fd)
	if err := syscall.EpollCtl(efd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
		log.Fatal("epoll_ctl_add ", err)
	}
}

func allocateDgramSocket(efd int) int {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.O_NONBLOCK|syscall.SOCK_DGRAM, 0)
	if err != nil {
		log.Fatal("allocateDgramSocket socket ", err)
	}
	if fd < 0 {
		log.Fatal("fd < 0")
	}

	addr := syscall.SockaddrInet4{Port: 0}
	n := copy(addr.Addr[:], net.ParseIP("0.0.0.0").To4())
	if n != 4 {
		log.Fatal("copy addr not 4 bytes")
	}

	syscall.Bind(fd, &addr)
	syscall.Listen(fd, 1)

	return fd
}

func (node *node) generateTid() {
	s := fmt.Sprintf("%s", string(rand.Intn(255)))
	node.tid = s
}
