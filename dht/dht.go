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

	"github.com/anacrolix/torrent/bencode"
)

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
type compactAddr string

const (
	live nodeStatus = iota
	dead
)

type node struct {
	id         nodeid
	address    compactAddr
	lastActive time.Time
	status     nodeStatus
	fd         int
}

// Dht maintain running status.
type Dht struct {
	efd    int
	selfid nodeid
	status dhtStatus
	nodes  []node
}

type findNodeMsg struct {
	T string `bencode:"t"`
	Y string `bencode:"y"`
	Q string `bencode:"q"`
	A struct {
		ID     string `bencode:"id"`
		TARGET string `bencode:"target"`
	} `bencode:"a"`
}

// New Create a listening socket, allocate spaces for storing nodes and dht status.
func New() *Dht {
	dht := new(Dht)

	dht.selfid = generateRandNodeid()
	// push bootstrap node into dht.

	dht.nodes = append(dht.nodes, node{address: convertToCompactAddr(bootstrap1), lastActive: time.Now(), status: live})

	efd, err := syscall.EpollCreate1(0)
	if err != nil {
		log.Fatal("epoll_create1()")
	}
	dht.efd = efd

	dht.status = suspended

	return dht
}

func (ca compactAddr) toSockAddr() syscall.SockaddrInet4 {
	ip1 := []byte(ca)[:4]
	port1 := []byte(ca)[4:6]

	addr := syscall.SockaddrInet4{Port: int(binary.BigEndian.Uint16(port1))}

	binary.LittleEndian.PutUint32(addr.Addr[:], binary.BigEndian.Uint32(ip1))
	return addr
}

func convertToCompactAddr(s string) compactAddr {
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

	return compactAddr(append(ip2, port2...))
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
	ticker := time.NewTicker(2 * time.Second)

	for {
		select {
		case <-ticker.C:
			log.Print("tic, SendFindNodeRandom")
			dht.SentFindNodeRandom()
		default:
			time.Sleep(1 * time.Second)

			dht.waitEpoll(1000)
		}
	}

	// ticker.Stop()
}
func (dht *Dht) waitEpoll(timeout int) {
	var events [MaxEpollEvents]syscall.EpollEvent

	nevents, err := syscall.EpollWait(dht.efd, events[:], timeout)
	if err != nil {
		log.Fatal("epoll_wait ", err)
	}

	for i := 0; i < nevents; i++ {
		fd := int(events[i].Fd)

		buf := make([]byte, 1500)
		n, fromAddr, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			log.Fatal(err)
		}
		processRecv(fd, buf, n, fromAddr)

	}
}

func processRecv(fd int, buf []byte, n int, fromAddr syscall.Sockaddr) {
	log.Println(fd, n, fromAddr)
}

// SentFindNodeRandom sent find_node query to random node for random target.
func (dht *Dht) SentFindNodeRandom() {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.O_NONBLOCK|syscall.SOCK_DGRAM, 0)
	if err != nil {
		log.Fatal(err)
	}

	addr := syscall.SockaddrInet4{Port: 0}
	n := copy(addr.Addr[:], net.ParseIP("0.0.0.0").To4())
	if n != 4 {
		log.Fatal("copy addr not 4 bytes")
	}

	syscall.Bind(fd, &addr)
	syscall.Listen(fd, 8)

	targetNode := dht.getRandomNode()
	targetNode.fd = fd

	tid := generateTid()
	msg := findNodeMsg{T: tid, Y: "q", Q: "find_node", A: struct {
		ID     string `bencode:"id"`
		TARGET string `bencode:"target"`
	}{ID: string(dht.selfid.toString()), TARGET: generateRandNodeid().toString()}}
	payload := bencode.MustMarshal(msg)
	log.Printf("FindNodeRandom , marshal: %s", payload)

	dstAddr := targetNode.address.toSockAddr()
	err = syscall.Sendto(fd, payload, 0, &dstAddr)
	if err != nil {
		log.Fatal("sendto ", err)
	}

	var event syscall.EpollEvent

	event.Events = syscall.EPOLLIN | EPOLLET
	event.Fd = int32(fd)
	if err := syscall.EpollCtl(dht.efd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
		log.Fatal(err)
	}

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
