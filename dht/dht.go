// Package dht implementions. It maintain neighbor nodes and keep farming.
package dht

import (
	"fmt"
	"log"
	"syscall"
	"time"
)

const dhtVersionMsg = "A2\x00\x03"

const farmTimeout = 5 * time.Second
const jobNodeTimeout = 3 * time.Second
const peerSynTimeout = 10 * time.Second
const peerConnectedTimeout = 60 * time.Second
const epollTimeout = 200

const examplehash = "4e84408183c0a37a3f26f871699b98bf6f66e07b"

//const examplehash = "0000000000000000000000000000000000000000"
const maxNodeNum = 50
const concurrentConnPerJob = 10000
const maxPeersPerJob = 1000
const neighborLen = 8

const (
	// EPOLLET uint32
	EPOLLET = 1 << 31
	// MaxEpollEvents num
	MaxEpollEvents = 32
)

// router.utorrent.com
var bootstraps = []string{
	"router.bittorrent.com:6881",
	"router.utorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"dht.aelitis.com:6881",
}

type dhtStatus int

const (
	running dhtStatus = iota
	suspended
)

// Dht maintain running status.
type Dht struct {
	efd      int
	selfid   nodeid
	status   dhtStatus
	jobs     []*job
	dummyJob *job
}

type fdCarrier interface {
	onEvent(int, int)
}

// Run send packets while listening active node port.
func (dht *Dht) Run() {
	farmTicker := time.NewTicker(10 * time.Second)
	statsTicker := time.NewTicker(5 * time.Second)

	// to be deleted
	dht.jobs = append(dht.jobs, &job{priv: dht, hash: stringToInfohash(examplehash), peers: make([]compactAddr, 0), status: gettingPeers, visitedPeers: make(map[string]bool), checkCloser: false})

	dht.farm()
	for {
		select {
		case <-farmTicker.C:
			dht.farm()
		case <-statsTicker.C:
			dht.PrintStats()
		default:
			dht.waitEpoll(epollTimeout)
			dht.afterSleep()
		}
	}

	// ticker.Stop()
}

func (dht *Dht) afterSleep() {
	dht.doJobs()
}

// New Create a listening socket, allocate spaces for storing nodes and dht status.
func New() *Dht {
	dht := new(Dht)

	dht.selfid = generateRandNodeid()
	dht.dummyJob = &job{priv: dht}

	efd, err := syscall.EpollCreate1(0)
	if err != nil {
		log.Fatal("epoll_create1()")
	}
	dht.efd = efd

	dht.status = suspended

	return dht
}

// FreeDht free allocated space.
//func (dht *Dht) FreeDht() {
//	syscall.Close(dht.efd)
//
//	for _, node := range dht.dummyJob.nodes {
//		err := syscall.Close(node.fd)
//		if err != nil {
//			log.Fatal(err)
//		}
//	}
//
//	return
//}
//
func (dht *Dht) farm() {
	if len(dht.dummyJob.nodes) == 0 {
		// push bootstrap node into dht.
		for _, bootstrap := range bootstraps {
			dht.dummyJob.appendNodeFromCompactAddr(convertToCompactAddr(bootstrap), generateRandNodeid())
		}
	}

	node := dht.dummyJob.getNodeByStatus(newAdded)
	if node == nil {
		node = dht.dummyJob.getNodeByStatus(receivedPacket)
		if node == nil {
			log.Panic("getNode return nil")
		}
	}

	dht.dummyJob.SendFindNode(node, stringToNodeID(examplehash))
	dht.cleanNodes()
}

func (dht *Dht) doJobs() {
	for _, job := range dht.jobs {
		job.doJob()
	}
}

func (dht *Dht) waitEpoll(timeout int) {
	var epEvents [MaxEpollEvents]syscall.EpollEvent

	nevents, err := syscall.EpollWait(dht.efd, epEvents[:], timeout)
	if err != nil {
		if err != syscall.EINTR {
			log.Fatal("epoll_wait ", err)
		}
	}

	for i := 0; i < nevents; i++ {
		fd := int(epEvents[i].Fd)
		events := int(epEvents[i].Events)
		fdCarrier := dht.findNodeByFd(fd)

		fdCarrier.onEvent(fd, events)
	}
}

func (dht *Dht) cleanNodes() {
	// delete timeout nodes
	for i := len(dht.dummyJob.nodes) - 1; i > -1; i-- {
		node := dht.dummyJob.nodes[i]
		if node.status == waitForPacket {
			if time.Now().Sub(node.lastQuery) > farmTimeout {
				dht.dummyJob.removeNode(i)
			}
		}
	}

	// delete unhealthy nodes
	for i := len(dht.dummyJob.nodes) - 1; i > -1; i-- {
		node := dht.dummyJob.nodes[i]
		if node.status == unhealthy {
			dht.dummyJob.removeNode(i)
		}
	}

	if len(dht.dummyJob.nodes) < 20 {
		return
	}

	// keep nodes num below maxNodeNum
	for {
		if len(dht.dummyJob.nodes) > maxNodeNum {
			i, _ := dht.dummyJob.getMinLastActiveNodeByStatus(newAdded)
			if i == -1 {
				i, _ = dht.dummyJob.getMinLastActiveNodeByStatus(receivedPacket)
			}
			if i < 0 {
				log.Panic("can not get node, i < 0")
			}

			dht.dummyJob.removeNode(i)
		} else {
			break
		}
	}

}

func (dht *Dht) findNodeByFd(fd int) fdCarrier {
	if !(fd > 2) {
		log.Fatal("findNodeByFd, invalid fd")
	}

	for _, node := range dht.dummyJob.nodes {
		if node.fd == fd {
			return node
		}
	}

	for _, job := range dht.jobs {
		for _, node := range job.nodes {
			if node.fd == fd {
				return node
			}
		}
	}

	for _, job := range dht.jobs {
		for _, peerConn := range job.peerConns {
			if peerConn.fd == fd {
				return peerConn
			}
		}
	}

	return nil
}

// PrintStats print internal information.
func (dht *Dht) PrintStats() string {
	job := dht.dummyJob

	s := "\n\n"
	s += fmt.Sprintf("dummyJob: newAdded: %d, waitForPacket: %d, receivedPacket: %d, unhealthy: %d\n", job.countNodesByStatus(newAdded), job.countNodesByStatus(waitForPacket), job.countNodesByStatus(receivedPacket), job.countNodesByStatus(unhealthy))

	for _, job := range dht.jobs {
		s += fmt.Sprintf("hashJob: newAdded: %d, waitForPacket: %d, receivedPacket: %d, unhealthy: %d, peers: %d, triedpeers: %d\n", job.countNodesByStatus(newAdded), job.countNodesByStatus(waitForPacket), job.countNodesByStatus(receivedPacket), job.countNodesByStatus(unhealthy), len(job.peers), len(job.visitedPeers))
	}

	s += "\n"

	log.Printf("%s", s)
	return s
}
