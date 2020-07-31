package dht

import (
	"fmt"
	"log"
	"strings"
	"syscall"
	"time"

	"github.com/anacrolix/torrent/bencode"
)

type infohash [20]byte

type jobStatus int

const (
	gettingPeers jobStatus = 1 << iota
	done
)

type job struct {
	hash         infohash
	metadata     string
	peers        []compactAddr
	peerConns    []*peerConn
	nodes        []*node // last node is tne one been queried or to be queried.
	status       jobStatus
	priv         *Dht
	visitedPeers map[string]bool
}

func (job *job) doJob() {
	job.download()

	if job.status == gettingPeers {

	findNode:
		for {
			if len(job.nodes) > 100 {
				break findNode
			}

			if len(job.nodes) == 0 {
				node := job.getNodeFromDummy()
				if node == nil {
					break findNode
				}
				job.appendNode(node)
			}
			node := job.nodes[len(job.nodes)-1]

			switch node.status {
			case newAdded:
				job.SendFindNode(node, stringToNodeID(examplehash))
				break findNode
			case waitForPacket:
				if node.sentType == "find_node" {
					if time.Now().Sub(node.lastQuery) > jobNodeTimeout {
						job.removeNode(len(job.nodes) - 1)
						// this node timeout, try next one
					} else {
						break findNode
					}
				} else if node.sentType == "get_peers" {
					s := ""
					for _, node := range job.nodes {
						s += fmt.Sprintf("[%s, %d]", node.sentType, node.status)
					}
					log.Panicf("unexpected sentType get_peer %s", s)
				} else {
					log.Panicf("unexpected sentType, %s", node.sentType)
				}
			case receivedPacket:
				node := job.getNodeFromDummy()
				if node == nil {
					break findNode
				}
				job.appendNode(node)
			case unhealthy:
				job.removeNode(len(job.nodes) - 1)
			default:
				log.Panicf("find node node status not expected %#v", node)
			}

		}

	getPeers:
		for {
			if len(job.nodes) < 50 {
				break getPeers
			}

			node := job.nodes[0]
			if node == nil {
				log.Print("getjoblastnode return nil")
				return
			}

			switch node.status {
			case newAdded:
				job.SendGetPeers(node)
				break getPeers
			case waitForPacket:
				if time.Now().Sub(node.lastQuery) > jobNodeTimeout {
					job.removeNode(0)
					// this node timeout, try next one
				} else {
					break getPeers
				}
			case receivedPacket:
				if node.sentType == "get_peers" {
					s := ""
					for _, node := range job.nodes {
						s += fmt.Sprintf("[%s, %d]", node.sentType, node.status)
					}
					log.Panicf("in get_peers unexpected sentType get_peer %s", s)
				} else if node.sentType == "find_node" {
					job.SendGetPeers(node)
					break getPeers
				} else {
					log.Panic("unknown sentType")
				}
			case unhealthy:
				job.removeNode(0)
				// problematic node, try next one
			default:
				log.Fatalf("get peer node not valid status, job: %#v", node)
			}
		}
	} else {
		log.Fatalf("doJob not valid status, job: %#v", job)
	}
}

func (job *job) download() {
	job.cleanPeerConn()
	job.populatePeerConn()

	for _, peerConn := range job.peerConns {
		peerConn.parseBuf()
		peerConn.sendOutBuf()
	}
}

func (job *job) populatePeerConn() {
	for {
		if len(job.peers) == 0 {
			return
		}
		if len(job.peerConns) >= concurrentConnPerJob {
			return
		}

		job.peerConns = append(job.peerConns, newPeerConn(job, job.peers[0]))
		job.peers = job.peers[1:len(job.peers)]
	}
}

func (job *job) cleanPeerConn() {
	for i := len(job.peerConns) - 1; i > -1; i-- {
		if job.peerConns[i].useless() {
			job.removePeerConn(i)
		}
	}

}
func (job *job) processGetPeersRes(node *node, buf []byte) error {
	type getPeersRes struct {
		T string `bencode:"t"`
		Y string `bencode:"y"`
		R struct {
			ID     string   `bencode:"id"`
			Token  string   `bencode:"token"`
			Values []string `bencode:"values"`
			Nodes  string   `bencode:"nodes"`
		} `bencode:"r"`
	}

	res := &getPeersRes{}
	err := bencode.Unmarshal(buf, res)
	if err != nil {
		if _, ok := err.(bencode.ErrUnusedTrailingBytes); !ok {
			log.Fatal("processRecv ", err)
		}
	}

	err = node.checkTid(res.T)
	if err != nil {
		node.setStatus(unhealthy)
		return err
	}

	// for check propose
	node.setStatus(receivedPacket)
	job.removeNode(0)

	job.appendPeers(res.R.Values)

	return nil
}

func (job *job) processFindNodeRes(node *node, buf []byte) error {
	type findNodeRes struct {
		T string `bencode:"t"`
		Y string `bencode:"y"`
		R struct {
			ID    string `bencode:"id"`
			Nodes string `bencode:"nodes"`
		} `bencode:"r"`
	}

	res := &findNodeRes{}
	err := bencode.Unmarshal(buf, res)
	if err != nil {
		if _, ok := err.(bencode.ErrUnusedTrailingBytes); !ok {
			log.Fatal("processRecv ", err)
		}
	}

	err = node.checkTid(res.T)
	if err != nil {
		node.setStatus(unhealthy)
		return err
	}

	checkCloser := false
	if job.priv.dummyJob == job {
		checkCloser = true
	}
	job.appendCompactAddrs(res.R.Nodes, checkCloser)

	node.setStatus(receivedPacket)

	return nil
}

// SendGetPeers find the peer having the info hash
func (job *job) SendGetPeers(node *node) {
	dht := job.priv

	type getPeerReqA struct {
		ID       string `bencode:"id"`
		InfoHash string `bencode:"info_hash"`
	}
	type getPeersReq struct {
		T string      `bencode:"t"`
		Y string      `bencode:"y"`
		Q string      `bencode:"q"`
		A getPeerReqA `bencode:"a"`
	}

	node.generateTid()
	msg := getPeersReq{T: node.tid, Y: "q", Q: "get_peers", A: getPeerReqA{ID: string(dht.selfid.toString()), InfoHash: string(job.hash[:])}}
	payload := bencode.MustMarshal(msg)

	node.allocateFd(dht.efd)
	fd := node.fd

	dstAddr := node.address.toSockAddr()
	log.Printf("send get_peers to ip: %v", dstAddr.Addr)
	err := syscall.Sendto(fd, payload, 0, &dstAddr)
	if err != nil {
		log.Fatalf("sendto fd: %d, err: %v", fd, err)
	}

	node.sentType = "get_peers"
	node.setStatus(waitForPacket)
}

// SendFindNode sent find_node query
func (job *job) SendFindNode(toNode *node, targetID nodeid) {
	dht := job.priv

	type findNodeReqA struct {
		ID     string `bencode:"id"`
		TARGET string `bencode:"target"`
	}
	type findNodeReq struct {
		T string       `bencode:"t"`
		Y string       `bencode:"y"`
		Q string       `bencode:"q"`
		A findNodeReqA `bencode:"a"`
	}

	toNode.generateTid()
	msg := findNodeReq{T: toNode.tid, Y: "q", Q: "find_node", A: findNodeReqA{ID: string(dht.selfid.toString()), TARGET: targetID.toString()}}
	payload := bencode.MustMarshal(msg)

	toNode.allocateFd(dht.efd)
	fd := toNode.fd

	dstAddr := toNode.address.toSockAddr()
	log.Printf("send find_node to ip: %v", dstAddr.Addr)
	err := syscall.Sendto(fd, payload, 0, &dstAddr)
	if err != nil {
		log.Fatalf("sendto fd: %d, err: %v", fd, err)
	}

	toNode.sentType = "find_node"
	toNode.setStatus(waitForPacket)
}

func (job *job) removePeerConn(i int) {
	if i < 0 {
		return
	}

	peerConn := job.peerConns[i]
	if peerConn.fd != -1 {
		err := syscall.Close(peerConn.fd)
		if err != nil {
			log.Fatal("close ", err)
		}
	}

	job.peerConns[i] = job.peerConns[len(job.peerConns)-1]
	job.peerConns = job.peerConns[:len(job.peerConns)-1]
}

func (job *job) removeNode(i int) {
	if i < 0 {
		return
	}

	node := job.nodes[i]
	if node.fd != -1 {
		// only waitForPacket nodes have valid fds
		if node.status != waitForPacket {
			log.Fatal("node status not waitforpacket")
		}

		err := syscall.Close(node.fd)
		if err != nil {
			log.Fatal("close ", err)
		}
	}

	for ; i < len(job.nodes)-1; i++ {
		job.nodes[i] = job.nodes[i+1]
	}

	job.nodes = job.nodes[:len(job.nodes)-1]
}

func (job *job) getNodeFromDummy() *node {
	node := job.priv.dummyJob.getNodeByStatus(receivedPacket)

	if node == nil {
		return nil
	}

	copiedNode := *node

	copiedNode.fd = -1
	copiedNode.status = newAdded
	copiedNode.priv = job

	return &copiedNode
}

func (job *job) getFirstNode() *node {
	if len(job.nodes) == 0 {
		return nil
	}

	return job.nodes[0]
}

func (job *job) getMinLastActiveNodeByStatus(status nodeStatus) (int, *node) {
	index := -1
	var res *node = nil
	minLastActive := time.Now()
	for i, node := range job.nodes {
		if (node.status&status) == node.status && minLastActive.After(node.lastActive) {
			index = i
			res = node
		}
	}

	return index, res
}

func (job *job) appendNode(node *node) {
	job.nodes = append(job.nodes, node)
}

func (job *job) appendNodeFromCompactAddr(addr compactAddr, id nodeid) {
	for _, node := range job.nodes {
		if id == node.id || addr == node.address {
			return
		}
	}

	log.Printf("%x", id)
	lastActive := time.Now()
	job.nodes = append(job.nodes, &node{priv: job, id: id, fd: -1, address: addr, lastActive: lastActive, status: newAdded})
}

func (job *job) getNodeByStatus(status nodeStatus) *node {
	for _, node := range job.nodes {
		if (node.status & status) == node.status {
			return node
		}
	}

	return nil
}

func (job *job) closerToHash(targetid nodeid) bool {
	lastNode := job.nodes[len(job.nodes)-1]
	if lastNode == nil {
		return true
	}

	var res1 [20]byte
	var res2 [20]byte
	var hash [20]byte
	var id [20]byte

	hash = job.hash

	id = lastNode.id
	for i := 0; i < 20; i++ {
		res1[i] = hash[i] ^ id[i]
	}

	id = targetid
	for i := 0; i < 20; i++ {
		res2[i] = hash[i] ^ id[i]
	}

	for i := 0; i < 20; i++ {
		if res1[i] == res2[i] {
			continue
		}

		return res1[i] > res2[i]
	}

	return false
}

func (job *job) appendCompactAddrs(cn string, checkCloser bool) {
	compactNodes := []byte(cn)

	if len(compactNodes)%26 != 0 {
		log.Fatal("len(compactNodes) % 26 != 0")
	}

	for i := 0; i < len(compactNodes)/26; i++ {
		info := compactNodes[i*26 : i*26+26]

		var id nodeid
		var addr compactAddr

		copy(id[:], info[0:20])
		copy(addr[:], info[20:26])

		if !checkCloser || job.closerToHash(id) {
			job.appendNodeFromCompactAddr(addr, id)
		}
	}
}

func (job *job) countNodesByStatus(status nodeStatus) int {
	n := 0
	for _, node := range job.nodes {
		if node.status == status {
			n++
		}
	}
	return n
}

func (job *job) appendPeers(peers []string) {
	m := job.visitedPeers

	for _, peer := range peers {
		if len(peer) != 6 {
			log.Panic("peer len not 6")
		}

		_, ok := m[peer]
		if ok {
			continue
		}
		m[peer] = true

		var ca compactAddr
		copy(ca[:], peer)

		if strings.Split(ca.toDialAddr(), ":")[0] == "24.224.204.197" {
			log.Panic("dst captured")
		}
		job.peers = append(job.peers, ca)
	}
}
