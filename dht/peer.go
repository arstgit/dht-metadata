package dht

import (
	"log"
	"syscall"
)

type peerConnStatus int

const (
	connecting peerConnStatus = 1 << iota
)

type peerConn struct {
	priv   *job
	addr   compactAddr
	fd     int
	buf    []byte
	status peerConnStatus
}

func newPeerConn(job *job, peer compactAddr) *peerConn {
	peerConn := &peerConn{fd: -1, addr: peer, status: connecting, priv: job}

	peerConn.allocateFd(job.priv.efd)

	addr := peer.toSockAddr()
	log.Printf("%#v", addr)
	log.Printf("%d", peerConn.fd)
	err := syscall.Connect(peerConn.fd, &addr)
	if err != nil {
		if err != syscall.EINPROGRESS {
			log.Panic("connect ", err)
		}
	}

	return peerConn
}

func (peerConn *peerConn) allocateFd(efd int) {
	fd := peerConn.fd
	if fd == -1 {
		fd = allocateSocket(syscall.SOCK_STREAM, false)
	}
	peerConn.fd = fd

	var event syscall.EpollEvent

	event.Events = syscall.EPOLLIN | syscall.EPOLLOUT | EPOLLET
	event.Fd = int32(fd)
	if err := syscall.EpollCtl(efd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
		log.Fatal("epoll_ctl_add ", err)
	}
}
