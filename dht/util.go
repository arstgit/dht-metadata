package dht

import (
	"encoding/binary"
	"encoding/hex"
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

type compactAddr [6]byte

func (ca compactAddr) toDialAddr() string {
	port := int(binary.BigEndian.Uint16(ca[4:6]))
	return fmt.Sprintf("%d.%d.%d.%d:%d", int(ca[0]), int(ca[1]), int(ca[2]), int(ca[3]), port)
}

func (ca compactAddr) toSockAddr() syscall.SockaddrInet4 {
	ip1 := ca[:4]
	port1 := ca[4:6]

	addr := syscall.SockaddrInet4{Port: int(binary.BigEndian.Uint16(port1))}

	copy(addr.Addr[:], ip1)
	return addr
}

func generateRandPeerid() nodeid {
	id := generate20Bytes()

	// Azureus-style uses the following encoding: '-', two characters for client id, four ascii digits for version number, '-', followed by random numbers.
	// -qB4250-
	nodeidPrefix := "2d7142343235302d"
	prefixBuf, err := hex.DecodeString(nodeidPrefix)
	if err != nil {
		log.Panic("decode nodeidprefix")
	}

	copy(id[0:8], prefixBuf)

	return id
}

func generate20Bytes() [20]byte {
	var id [20]byte
	rand.Seed(time.Now().UnixNano())
	r := rand.Uint32()
	for i := 0; i < 5; i++ {
		copy(id[i*4:], (*[4]byte)(unsafe.Pointer(&r))[:])
	}

	return id
}

func generateRandNodeid() nodeid {
	return generate20Bytes()
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

func stringToNodeID(s string) nodeid {
	if len(s) != 40 {
		log.Fatal("nodeid len not 40")
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

func allocateSocket(flag int, listen bool) int {
	flag = syscall.O_NONBLOCK | flag

	fd, err := syscall.Socket(syscall.AF_INET, flag, 0)
	if err != nil {
		log.Fatal("allocateDgramSocket socket ", err)
	}
	if fd < 0 {
		log.Fatal("fd < 0")
	}

	if listen {
		addr := syscall.SockaddrInet4{Port: 0}
		n := copy(addr.Addr[:], net.ParseIP("0.0.0.0").To4())
		if n != 4 {
			log.Fatal("copy addr not 4 bytes")
		}

		syscall.Bind(fd, &addr)
		syscall.Listen(fd, 1)
	}

	return fd
}
