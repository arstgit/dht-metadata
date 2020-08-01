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

func bitFirstBigger(id1 [20]byte, id2 [20]byte) bool {
	for i := 0; i < 20; i++ {
		if id1[i] > id2[i] {
			return true
		}
		if id1[i] < id2[i] {
			return false
		}
	}

	return false
}

func bitOp(operator string, id1 [20]byte, id2 [20]byte) [20]byte {
	var res [20]byte

	switch operator {
	case "xor":
		for i := 0; i < 20; i++ {
			res[i] = id1[i] ^ id2[i]
		}
	case "add":
		carry := byte(0x00)
		for i := 160 - 1; i > -1; i-- {
			byteIndex := i / 8
			offset := 7 - (i % 8)
			op1 := (id1[byteIndex] >> offset) & 0x01
			op2 := (id2[byteIndex] >> offset) & 0x01
			sum := op1 + op2 + carry
			r := sum & 0x01
			carry = (sum >> 1) & 0x01
			res[byteIndex] |= (r << offset)
		}
	default:
		log.Panicf("invalid operator %s", operator)
	}

	return res
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
