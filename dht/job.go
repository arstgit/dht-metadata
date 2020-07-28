package dht

import (
	"log"
	"syscall"
	"time"

	"github.com/anacrolix/torrent/bencode"
)

const examplehash = "0271e29577a33ce47dc38c78240f2363bcb4ef7c"

type infohash [20]byte

type jobStatus int

const (
	gettingPeers jobStatus = 1 << iota
	done
)

type job struct {
	hash     infohash
	metadata string
	peers    []string
	nodes    []*node // last node is tne one been queried or to be queried.
	status   jobStatus
	priv     *Dht
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

	node.setStatus(receivedPacket)

	// got peers
	if len(res.R.Values) > 0 {
		job.peers = append(job.peers, res.R.Values...)
		log.Printf("peers: %#v", job.peers)
	}

	job.removeLastNode()
	job.appendCompactNodes(res.R.Nodes)

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

	job.appendCompactNodes(res.R.Nodes)

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

// SendFindNodeRandom sent find_node query to random node for random target.
func (job *job) SendFindNodeRandom() {
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

	node := job.priv.dummyJob.getNodeByStatus(newAdded)
	if node == nil {
		node = job.priv.dummyJob.getNodeByStatus(receivedPacket)
		if node == nil {
			log.Fatal("getNode return nil")
		}
	}

	node.generateTid()
	msg := findNodeReq{T: node.tid, Y: "q", Q: "find_node", A: findNodeReqA{ID: string(dht.selfid.toString()), TARGET: generateRandNodeid().toString()}}
	payload := bencode.MustMarshal(msg)

	node.allocateFd(dht.efd)
	fd := node.fd

	dstAddr := node.address.toSockAddr()
	log.Printf("send find_node to ip: %v", dstAddr.Addr)
	err := syscall.Sendto(fd, payload, 0, &dstAddr)
	if err != nil {
		log.Fatalf("sendto fd: %d, err: %v", fd, err)
	}

	node.sentType = "find_node"
	node.setStatus(waitForPacket)
}

func (job *job) removeLastNode() {
	job.removeNode(len(job.nodes) - 1)
}

func (job *job) removeNode(i int) {
	if i < 0 {
		return
	}

	node := job.nodes[i]
	if node.fd != -1 {
		if node.status != waitForPacket {
			log.Fatal("node status not waitforpacket")
		}

		err := syscall.Close(node.fd)
		if err != nil {
			log.Fatal("close ", err)
		}
	}

	job.nodes[i] = job.nodes[len(job.nodes)-1]
	job.nodes = job.nodes[:len(job.nodes)-1]
}

func (job *job) getLastNode() *node {
	if len(job.nodes) == 0 {
		node := job.priv.dummyJob.getNodeByStatus(receivedPacket)

		if node == nil {
			return nil
		}

		copiedNode := *node
		copiedNode.status = newAdded
		copiedNode.priv = job
		job.nodes = append(job.nodes, &copiedNode)
	}

	return job.nodes[len(job.nodes)-1]
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

func (job *job) appendNode(addr compactAddr, id nodeid) {
	for _, node := range job.nodes {
		if id == node.id || addr == node.address {
			return
		}
	}

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

func (job *job) appendCompactNodes(cn string) {
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

		job.appendNode(addr, id)
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
