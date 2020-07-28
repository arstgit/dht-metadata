// Package dht implementions. It maintain neighbor nodes and keep farming.
package dht

import (
	"fmt"
	"log"
	"syscall"
	"time"
)

const maxNodeNum = 100
const jobTimeout = 5 * time.Second
const farmTimeout = 10 * time.Second

const (
	// EPOLLET uint32
	EPOLLET = 1 << 31
	// MaxEpollEvents num
	MaxEpollEvents = 32
)

// router.utorrent.com
var bootstraps = []string{"router.bittorrent.com:6881"}

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

// Run send packets while listening active node port.
func (dht *Dht) Run() {
	farmTicker := time.NewTicker(10 * time.Second)
	cleanTicker := time.NewTicker(5 * time.Second)
	jobTicker := time.NewTicker(5 * time.Second)
	epollTimeout := 1000

	// to be deleted
	dht.jobs = append(dht.jobs, &job{priv: dht, hash: stringToInfohash(examplehash), peers: make([]string, 0), status: gettingPeers})

	for {
		select {
		case <-farmTicker.C:
			dht.farm()
		case <-cleanTicker.C:
			dht.cleanNodes()
		case <-jobTicker.C:
			dht.doJobs()
		default:
			dht.waitEpoll(epollTimeout)

			dht.PrintStats()
		}
	}

	// ticker.Stop()
}

// New Create a listening socket, allocate spaces for storing nodes and dht status.
func New() *Dht {
	dht := new(Dht)

	dht.selfid = generateRandNodeid()
	dht.dummyJob = &job{priv: dht}

	// push bootstrap node into dht.
	for _, bootstrap := range bootstraps {
		dht.dummyJob.appendNode(convertToCompactAddr(bootstrap), generateRandNodeid())
	}

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
	dht.dummyJob.SendFindNodeRandom()
}

func (dht *Dht) doJob(job *job) {
	if job.status == gettingPeers {

	thisJob:
		for {
			node := job.getLastNode()
			if node == nil {
				log.Print("getjoblastnode return nil")
				return
			}

			switch node.status {
			case newAdded:
				job.SendGetPeers(node)
				break thisJob
			case waitForPacket:
				if time.Now().Sub(node.lastActive) > jobTimeout {
					job.removeNode(len(job.nodes) - 1)
				}
			default:
				log.Fatalf("node not valid status, job: %#v", node)
			}
		}
	} else {
		log.Fatalf("doJob not valid status, job: %#v", job)
	}
}

func (dht *Dht) doJobs() {
	for _, job := range dht.jobs {
		dht.doJob(job)
	}
}

func (dht *Dht) processRecv(fd int, buf []byte, n int, fromAddr syscall.Sockaddr) {
	node := dht.findNodeByFd(fd)

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

func (dht *Dht) waitEpoll(timeout int) {
	var events [MaxEpollEvents]syscall.EpollEvent

	nevents, err := syscall.EpollWait(dht.efd, events[:], timeout)
	if err != nil {
		if err != syscall.EINTR {
			log.Fatal("epoll_wait ", err)
		}
	}

	for i := 0; i < nevents; i++ {
		fd := int(events[i].Fd)
		buf := make([]byte, 1472)
		n, fromAddr, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			log.Fatal(err)
		}
		dht.processRecv(fd, buf, n, fromAddr)
	}
}

func (dht *Dht) cleanNodes() {
	// delete timeout nodes
	for i, node := range dht.dummyJob.nodes {
		if node.status == waitForPacket {
			if time.Now().Sub(node.lastActive) > farmTimeout {
				log.Panic()
				dht.dummyJob.removeNode(i)
			}
		}
	}

	// delete unhealthy nodes
	for i, node := range dht.dummyJob.nodes {
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

func (dht *Dht) findNodeByFd(fd int) *node {
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

	return nil
}

// PrintStats print internal information.
func (dht *Dht) PrintStats() string {
	job := dht.dummyJob

	s := "\n\n"
	s += fmt.Sprintf("dummyJob: newAdded: %d, waitForPacket: %d, receivedPacket: %d, unhealthy: %d\n", job.countNodesByStatus(newAdded), job.countNodesByStatus(waitForPacket), job.countNodesByStatus(receivedPacket), job.countNodesByStatus(unhealthy))

	for _, job := range dht.jobs {
		s += fmt.Sprintf("hashJob: newAdded: %d, waitForPacket: %d, receivedPacket: %d, unhealthy: %d, peers: %d\n", job.countNodesByStatus(newAdded), job.countNodesByStatus(waitForPacket), job.countNodesByStatus(receivedPacket), job.countNodesByStatus(unhealthy), len(job.peers))
	}

	s += "\n"

	log.Printf("%s", s)
	return s
}
