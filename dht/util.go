package dht

import (
	"encoding/binary"
	"encoding/hex"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type compactAddr [6]byte

func (ca compactAddr) toSockAddr() syscall.SockaddrInet4 {
	ip1 := ca[:4]
	port1 := ca[4:6]

	addr := syscall.SockaddrInet4{Port: int(binary.BigEndian.Uint16(port1))}

	copy(addr.Addr[:], ip1)
	return addr
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
	ips, err := net.LookupHost(arr[0])
	if err != nil {
		log.Fatal("lookuphost ", err)
	}

	ip := ips[0]
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

func stringToInfohash(s string) infohash {
	if len(s) != 40 {
		log.Fatal("infohash string len not 40")
	}

	var res [20]byte

	n, err := hex.Decode(res[:], []byte(s))
	if err != nil {
		log.Fatal(err)
	}
	if n != 20 {
		log.Fatal("decoded return n not 20")
	}

	return res
}
