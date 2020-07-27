package dht

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"syscall"
	"time"

	"github.com/anacrolix/torrent/bencode"
)

func (dht *Dht) processFindNodeRes(node *node, buf []byte) {
	type findNodeRes struct {
		T string `bencode:"t"`
		Y string `bencode:"y"`
		R struct {
			ID    string `bencode:"id"`
			NODES string `bencode:"nodes"`
		} `bencode:"r"`
	}

	res := &findNodeRes{}
	err := bencode.Unmarshal(buf, res)
	if err != nil {
		if _, ok := err.(bencode.ErrUnusedTrailingBytes); !ok {
			log.Fatal("processRecv ", err)
		}
	}

	if node.tid != res.T {
		log.Print("node.tid is not match res.T")
		node.status = unhealthy
		return
	}

	compactNodes := []byte(res.R.NODES)

	if len(compactNodes)%26 != 0 {
		log.Fatal("len(compactNodes) % 26 != 0")
	}

	for i := 0; i < len(compactNodes)/26; i++ {
		info := compactNodes[i*26 : i*26+26]

		var id nodeid
		var addr compactAddr

		copy(id[:], info[0:20])
		copy(addr[:], info[20:26])

		dht.addNode(addr, id)
	}

	err = syscall.Close(node.fd)
	if err != nil {
		log.Fatal("close ", err)
	}
	node.fd = -1
	node.status = verified
	node.lastActive = time.Now()
}

func (dht *Dht) processRecv(fd int, buf []byte, n int, fromAddr syscall.Sockaddr) {
	node := dht.findNodeByFd(fd)

	log.Printf("%#v", dht.nodes)
	if node == nil {
		log.Fatalf("node is nil, %d", fd)
	}
	if node.status != waitForPacket {
		log.Fatal("node status not waitforpacket")
	}

	switch node.sentType {
	case "find_node":
		dht.processFindNodeRes(node, buf)
	default:
		log.Fatal("node.sentType ", node.sentType)
	}
}

func allocateDgramSocket(efd int) int {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.O_NONBLOCK|syscall.SOCK_DGRAM, 0)
	if err != nil {
		log.Fatal("allocateDgramSocket socket ", err)
	}
	addr := syscall.SockaddrInet4{Port: 0}
	n := copy(addr.Addr[:], net.ParseIP("0.0.0.0").To4())
	if n != 4 {
		log.Fatal("copy addr not 4 bytes")
	}

	syscall.Bind(fd, &addr)
	syscall.Listen(fd, 1)

	var event syscall.EpollEvent

	event.Events = syscall.EPOLLIN | EPOLLET
	event.Fd = int32(fd)
	if err := syscall.EpollCtl(efd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
		log.Fatal("epoll_ctl_add ", err)
	}

	return fd
}

// SendFindNodeRandom sent find_node query to random node for random target.
func (dht *Dht) SendFindNodeRandom() {
	type findNodeReq struct {
		T string `bencode:"t"`
		Y string `bencode:"y"`
		Q string `bencode:"q"`
		A struct {
			ID     string `bencode:"id"`
			TARGET string `bencode:"target"`
		} `bencode:"a"`
	}

	tid := generateTid()
	msg := findNodeReq{T: tid, Y: "q", Q: "find_node", A: struct {
		ID     string `bencode:"id"`
		TARGET string `bencode:"target"`
	}{ID: string(dht.selfid.toString()), TARGET: generateRandNodeid().toString()}}
	payload := bencode.MustMarshal(msg)
	log.Printf("FindNodeRandom , marshal: %s", payload)

	_, targetNode := dht.getNode(newAdded | verified)

	dstAddr := targetNode.address.toSockAddr()

	fd := targetNode.fd
	if fd == -1 {
		fd = allocateDgramSocket(dht.efd)
	}

	log.Printf("sendto ip: %v", dstAddr.Addr)
	err := syscall.Sendto(fd, payload, 0, &dstAddr)
	if err != nil {
		log.Fatalf("sendto fd: %d, err: %v", fd, err)
	}

	targetNode.fd = fd
	targetNode.tid = tid
	targetNode.sentType = "find_node"
	targetNode.status = waitForPacket

	log.Printf("%#v", targetNode)
	log.Printf("%#v", dht.nodes)
}

func (ca compactAddr) toSockAddr() syscall.SockaddrInet4 {
	ip1 := ca[:4]
	port1 := ca[4:6]

	addr := syscall.SockaddrInet4{Port: int(binary.BigEndian.Uint16(port1))}

	copy(addr.Addr[:], ip1)
	return addr
}

func generateTid() string {
	s := fmt.Sprintf("%s", string(rand.Intn(256)))

	return s
}
