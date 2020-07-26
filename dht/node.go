package dht

import (
	"encoding/binary"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

type nodeid [20]byte

func (id nodeid) toString() string {
	var tmp = [20]byte(id)

	return string(tmp[:])
}

type compactAddr [6]byte

type nodeStatus int

const (
	newAdded nodeStatus = iota
	waitForPacket
	varified
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

func generateRandNodeid() nodeid {
	var id nodeid
	r := rand.Uint32()
	for i := 0; i < 5; i++ {
		copy(id[i*4:], (*[4]byte)(unsafe.Pointer(&r))[:])
	}

	return id
}

func convertToCompactAddr(s string) compactAddr {
	var addr compactAddr

	arr := strings.Split(s, ":")
	ip := arr[0]
	port := arr[1]

	ip1 := net.ParseIP(ip).To4()
	port1, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		log.Fatal(err)
	}

	port2 := make([]byte, 2)

	binary.BigEndian.PutUint16(port2, uint16(port1))

	copy(addr[:], append([]byte(ip1), port2...))
	return addr
}
