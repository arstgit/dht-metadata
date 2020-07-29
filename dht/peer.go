package dht

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"log"
	"syscall"
	"time"

	"github.com/anacrolix/torrent/bencode"
)

const handshakeProtocol = "13426974546f7272656e742070726f746f636f6c"
const handshakeLen = 1 + 19 + 8 + 20 + 20

type peerConnStatus int

const (
	connecting peerConnStatus = 1 << iota
	sentHandshake
	receivingHandshakeExtended
	receivedHandshakeExtended
	sentHandshakeExtended
	sentReq

	wrongExtendedMsg
	recvClosed
	connerror
)

type peerConn struct {
	priv          *job
	addr          compactAddr
	fd            int
	buf           []byte
	outBuf        []byte
	status        peerConnStatus
	lastQuery     time.Time
	extendedMsgID int
	metadataSize  int
}

type handshakeExtendedResM struct {
	UtMetadata int `bencode:"ut_metadata"`
}

func (p *peerConn) parseBuf() {
changingState:
	for {
		if p.status == sentHandshake {
			if len(p.buf) < handshakeLen {
				// continue receiving bytes, do nothing
				break changingState
			} else {
				handshakePrefix, err := hex.DecodeString(handshakeProtocol)
				if err != nil {
					log.Panic("encodeString ", err)
				}

				check := bytes.Equal(p.buf[0:20], handshakePrefix)
				if check != true {
					log.Panic("received content not expected, protocal")
				}
				check = bytes.Equal(p.priv.hash[:], p.buf[28:48])
				if check != true {
					log.Panic("received content not expected, hash")
				}

				p.buf = p.buf[68:]

				p.setStatus(receivingHandshakeExtended)
			}
		} else if p.status == receivingHandshakeExtended {
			/*	uint32_t 	length prefix. Specifies the number of bytes for the entire message. (Big endian)
				uint8_t 	bittorrent message ID, = 20
				uint8_t 	extended message ID. 0 = handshake, >0 = extended message as specified by the handshake.
			*/
			if len(p.buf) < 4 {
				break changingState
			}

			msgLen := binary.BigEndian.Uint32(p.buf)

			if len(p.buf) < int(msgLen+4) {
				break changingState
			}

			// message type not extended(20)
			if p.buf[4] != 0x14 {
				p.buf = p.buf[msgLen+4:]
				continue changingState
			}

			// to do
			if p.buf[5] != 0x00 {
				p.buf = p.buf[msgLen+4:]
				break changingState
			}

			type handshakeExtendedRes struct {
				M            handshakeExtendedResM `bencode:"m"`
				MetadataSize int                   `bencode:"metadata_size"`
			}

			msg := new(handshakeExtendedRes)
			err := bencode.Unmarshal(p.buf[4+1+1:msgLen+4], msg)
			if err != nil {
				if _, ok := err.(bencode.ErrUnusedTrailingBytes); !ok {
					log.Fatal("processRecv ", err)
				}
			}

			if msg.M.UtMetadata == 0 || msg.MetadataSize != 0 {
				p.setStatus(wrongExtendedMsg)
				break changingState
			}

			p.extendedMsgID = msg.M.UtMetadata
			p.metadataSize = msg.MetadataSize
			p.buf = p.buf[msgLen+4:]
			p.setStatus(receivedHandshakeExtended)
			// everything seems ok, continue processing
		} else if p.status == sentReq {
			log.Panicf("%#v", p)
		} else {
			break changingState
		}
	}
}

func (p *peerConn) sendOutBuf() {
outer:
	for {
		if p.status == connecting {
			buf := p.sendHandshake()

			p.outBuf = append(p.outBuf, buf...)
			p.setStatus(sentHandshake)
			break outer
		} else if p.status == receivedHandshakeExtended {
			type handshakeExtendedReq struct {
				M handshakeExtendedResM `bencode:"m"`
				P int                   `bencode:"p"`
				V string                `bencode:"v"`
			}
			msg := handshakeExtendedReq{M: handshakeExtendedResM{UtMetadata: p.extendedMsgID}, P: 6932, V: "qBittorrent/4.2.5"}
			payload := bencode.MustMarshal(msg)
			msgLen := 1 + len(payload)

			buf := make([]byte, 4+msgLen)
			binary.BigEndian.PutUint32(buf, uint32(msgLen))
			buf[4] = 0x14
			buf[5] = 0x00
			copy(buf[6:], payload)

			p.outBuf = append(p.outBuf, buf...)
			p.setStatus(sentHandshakeExtended)
		} else if p.status == sentHandshakeExtended {
			type metadataReq struct {
				MsgType int `bencode:"msg_type"`
				Piece   int `bencode:"piece"`
			}
			msg := metadataReq{MsgType: 0, Piece: 0}
			payload := bencode.MustMarshal(msg)
			msgLen := 1 + len(payload)

			buf := make([]byte, 4+msgLen)
			binary.BigEndian.PutUint32(buf, uint32(msgLen))
			buf[4] = 0x14
			buf[5] = byte(p.extendedMsgID)
			copy(buf[6:], payload)

			p.outBuf = append(p.outBuf, buf...)
			p.setStatus(sentReq)

			break outer
		} else {
			break outer
		}
	}

	p.write()
}

func newPeerConn(job *job, peer compactAddr) *peerConn {
	peerConn := &peerConn{fd: -1, addr: peer, status: connecting, priv: job, lastQuery: time.Now()}

	peerConn.allocateFd(job.priv.efd)

	addr := peer.toSockAddr()

	log.Printf("connecting to %s", peer.toDialAddr())

	err := syscall.Connect(peerConn.fd, &addr)
	if err != nil {
		if err != syscall.EINPROGRESS {
			log.Print("connect ", err)
		}
	}

	return peerConn
}

func (p *peerConn) setStatus(status peerConnStatus) {
	p.status = status
}

func (p *peerConn) onEvent(fd int, events int) {
	if (events & syscall.EPOLLERR) != 0 {
		value, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
		if err != nil {
			log.Panic("getsockoptint ", err)
		}

		log.Print("epollErr ", value)
		p.setStatus(connerror)
		return
	}

	if (events & syscall.EPOLLHUP) != 0 {
		log.Print("epollhup")
		p.setStatus(connerror)
		return
	}

	if (events & syscall.EPOLLIN) != 0 {
		log.Print("\x07")
		p.read()
	}

	if (events & syscall.EPOLLOUT) != 0 {
		p.write()
	}
}

func (p *peerConn) write() {
	if len(p.outBuf) == 0 {
		return
	}

	n, err := syscall.Write(p.fd, p.outBuf)
	if err != nil {
		// EAGAIN: 0xb
		if err.(syscall.Errno) != syscall.Errno(0xb) {
			log.Print("write ", err)

			p.setStatus(connerror)
			return
		}
		return
	}

	if n > 0 {
		p.lastQuery = time.Now()
		p.outBuf = p.outBuf[n:]
	}

}

func (p *peerConn) read() {
	buf := make([]byte, 1500)

	n, err := syscall.Read(p.fd, buf)
	if err != nil {
		log.Panic("read ", err)
	}
	if n == 0 {
		p.setStatus(recvClosed)
	}
	log.Panic("read n:", n)

	p.buf = append(p.buf, buf...)
	return

}

func (p *peerConn) sendHandshake() []byte {
	handshakePrefix, err := hex.DecodeString(handshakeProtocol)
	if err != nil {
		log.Panic("encodeString ", err)
	}
	extendPrefix, err := hex.DecodeString("0000000000100005")
	if err != nil {
		log.Panic("encodeString ", err)
	}

	hash, err := hex.DecodeString(examplehash)
	if err != nil {
		log.Panic("encodeString ", err)
	}

	var buf bytes.Buffer
	buf.Write(handshakePrefix)
	buf.Write(extendPrefix)
	buf.Write(hash)
	buf.Write(p.priv.priv.selfid[:])

	if buf.Len() != handshakeLen {
		log.Panic("send buf len not handshakeLen")
	}

	return buf.Bytes()
}

func (p *peerConn) allocateFd(efd int) {
	fd := p.fd
	if fd == -1 {
		fd = allocateSocket(syscall.SOCK_STREAM, false)
	}
	p.fd = fd

	err := syscall.SetsockoptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
	if err != nil {
		log.Panic("setsockoptint tcp nodelay", err)
	}

	var event syscall.EpollEvent

	event.Events = syscall.EPOLLIN | syscall.EPOLLOUT | EPOLLET
	event.Fd = int32(fd)
	if err := syscall.EpollCtl(efd, syscall.EPOLL_CTL_ADD, fd, &event); err != nil {
		log.Fatal("epoll_ctl_add ", err)
	}
}

func (p *peerConn) useless() bool {
	// checkout timeout or not
	if p.status == connecting {
		if time.Now().Sub(p.lastQuery) > peerSynTimeout {
			return true
		}
	} else {
		if time.Now().Sub(p.lastQuery) > peerConnectedTimeout {
			return true
		}
	}

	// unhealthy
	if (p.status & (connerror | recvClosed | wrongExtendedMsg)) != 0 {
		return true
	}

	return false
}
