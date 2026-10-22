package main

// ============================================================
//  x11.go - a tiny X11 client written in pure Go (stdlib
//  only, no cgo, no libraries). Just enough of the protocol
//  to open a real window on any Linux desktop, paint frames
//  into it with PutImage, and read the keyboard.
//  This is what makes AlbiPSX an actual Linux program.
// ============================================================

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

type xVisual struct {
	id                         uint32
	redMask, greenMask, blueMask uint32
}

type xConn struct {
	c          net.Conn
	br         *bufio.Reader
	bw         *bufio.Writer
	mu         sync.Mutex
	seq        uint16
	ridBase    uint32
	ridMask    uint32
	ridNext    uint32
	root       uint32
	rootVisual uint32
	blackPixel uint32
	maxReqLen  int // in 4-byte units
	minKey     byte
	maxKey     byte
	visuals    map[uint32]xVisual
	replies    map[uint16]chan []byte
	repMu      sync.Mutex
	events     chan []byte
	lastErr    string
	imgLSB     bool
}

func xParseDisplay() (string, string, error) {
	d := os.Getenv("DISPLAY")
	if d == "" {
		return "", "", fmt.Errorf("DISPLAY is not set")
	}
	// forms: :0, :0.0, localhost:10.0, unix:0
	colon := strings.LastIndex(d, ":")
	if colon < 0 {
		return "", "", fmt.Errorf("odd DISPLAY %q", d)
	}
	host := d[:colon]
	rest := d[colon+1:]
	if dot := strings.Index(rest, "."); dot >= 0 {
		rest = rest[:dot]
	}
	if host == "" || host == "unix" || host == "localhost" {
		return "unix", "/tmp/.X11-unix/X" + rest, nil
	}
	return "tcp", host + ":6000", nil // display number added below is rest-independent for our use
}

func xDial() (*xConn, error) {
	network, addr, err := xParseDisplay()
	if err != nil {
		return nil, err
	}
	if network == "tcp" {
		// host:6000 + display number
		d := os.Getenv("DISPLAY")
		colon := strings.LastIndex(d, ":")
		num := d[colon+1:]
		if dot := strings.Index(num, "."); dot >= 0 {
			num = num[:dot]
		}
		port := 6000
		fmt.Sscanf(num, "%d", &port)
		addr = fmt.Sprintf("%s:%d", d[:colon], 6000+port)
	}
	c, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	x := &xConn{
		c: c, br: bufio.NewReader(c), bw: bufio.NewWriter(c),
		visuals: map[uint32]xVisual{},
		replies: map[uint16]chan []byte{},
		events:  make(chan []byte, 128),
		imgLSB:  true,
	}
	if err := x.setup(); err != nil {
		c.Close()
		return nil, err
	}
	go x.reader()
	return x, nil
}

func (x *xConn) setup() error {
	// client setup: 'l' little-endian, protocol 11.0, no auth
	req := make([]byte, 12)
	req[0] = 'l'
	binary.LittleEndian.PutUint16(req[2:], 11)
	if _, err := x.c.Write(req); err != nil {
		return err
	}
	head := make([]byte, 8)
	if _, err := readFull(x.br, head); err != nil {
		return err
	}
	if head[0] == 0 {
		msgLen := int(binary.LittleEndian.Uint16(head[6:]))
		msg := make([]byte, msgLen*4)
		readFull(x.br, msg)
		return fmt.Errorf("X server refused: %s", string(msg))
	}
	if head[0] != 1 {
		return fmt.Errorf("X setup: unexpected status %d", head[0])
	}
	extra := int(binary.LittleEndian.Uint16(head[6:])) * 4
	body := make([]byte, extra)
	if _, err := readFull(x.br, body); err != nil {
		return err
	}
	x.ridBase = binary.LittleEndian.Uint32(body[4:])
	x.ridMask = binary.LittleEndian.Uint32(body[8:])
	vendorLen := int(binary.LittleEndian.Uint16(body[16:]))
	x.maxReqLen = int(binary.LittleEndian.Uint16(body[18:]))
	numRoots := int(body[20])
	numFormats := int(body[21])
	x.imgLSB = body[22] == 0
	x.minKey = body[26]
	x.maxKey = body[27]
	off := 32 + ((vendorLen + 3) &^ 3) + numFormats*8
	for r := 0; r < numRoots; r++ {
		root := binary.LittleEndian.Uint32(body[off:])
		rootVisual := binary.LittleEndian.Uint32(body[off+32:])
		rootDepth := int(body[off+38])
		numDepths := int(body[off+39])
		if r == 0 {
			x.root = root
			x.rootVisual = rootVisual
			x.blackPixel = binary.LittleEndian.Uint32(body[off+12:])
		}
		_ = rootDepth
		off += 40
		for d := 0; d < numDepths; d++ {
			nv := int(binary.LittleEndian.Uint16(body[off+2:]))
			off += 8
			for v := 0; v < nv; v++ {
				vis := xVisual{
					id:        binary.LittleEndian.Uint32(body[off:]),
					redMask:   binary.LittleEndian.Uint32(body[off+8:]),
					greenMask: binary.LittleEndian.Uint32(body[off+12:]),
					blueMask:  binary.LittleEndian.Uint32(body[off+16:]),
				}
				x.visuals[vis.id] = vis
				off += 24
			}
		}
	}
	return nil
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (x *xConn) reader() {
	for {
		head := make([]byte, 32)
		if _, err := readFull(x.br, head); err != nil {
			return
		}
		switch head[0] {
		case 0: // error
			x.lastErr = fmt.Sprintf("X error code %d (request seq %d, bad value %d)",
				head[1], binary.LittleEndian.Uint16(head[2:]), binary.LittleEndian.Uint32(head[4:]))
			fmt.Fprintln(os.Stderr, x.lastErr)
		case 1: // reply
			extra := int(binary.LittleEndian.Uint32(head[4:])) * 4
			full := head
			if extra > 0 {
				more := make([]byte, extra)
				if _, err := readFull(x.br, more); err != nil {
					return
				}
				full = append(full, more...)
			}
			seq := binary.LittleEndian.Uint16(full[2:])
			x.repMu.Lock()
			ch, ok := x.replies[seq]
			if ok {
				delete(x.replies, seq)
			}
			x.repMu.Unlock()
			if ok {
				ch <- full
			}
		default: // event
			select {
			case x.events <- head:
			default:
			}
		}
	}
}

// send writes one request and returns its sequence number.
func (x *xConn) send(req []byte) uint16 {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.seq++
	seq := x.seq
	x.bw.Write(req)
	x.bw.Flush()
	return seq
}

func (x *xConn) sendForReply(req []byte) []byte {
	x.repMu.Lock()
	x.mu.Lock()
	x.seq++
	seq := x.seq
	ch := make(chan []byte, 1)
	x.replies[seq] = ch
	x.mu.Unlock()
	x.repMu.Unlock()
	x.mu.Lock()
	x.bw.Write(req)
	x.bw.Flush()
	x.mu.Unlock()
	return <-ch
}

func (x *xConn) nextID() uint32 {
	x.ridNext++
	return x.ridBase | (x.ridNext & x.ridMask)
}

func reqHeader(opcode byte, b1 byte, lengthUnits uint16) []byte {
	r := make([]byte, 0, int(lengthUnits)*4)
	r = append(r, opcode, b1)
	var lb [2]byte
	binary.LittleEndian.PutUint16(lb[:], lengthUnits)
	r = append(r, lb[:]...)
	return r
}

func putU32(r []byte, v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return append(r, b[:]...)
}

func putU16(r []byte, v uint16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	return append(r, b[:]...)
}

func putI16(r []byte, v int16) []byte { return putU16(r, uint16(v)) }

func (x *xConn) createWindow(w, h int, title string) (uint32, uint32, error) {
	win := x.nextID()
	gc := x.nextID()
	// CreateWindow: depth 0 = CopyFromParent, visual 0 = CopyFromParent
	r := reqHeader(1, 0, 10)
	r = putU32(r, win)
	r = putU32(r, x.root)
	r = putI16(r, 80)
	r = putI16(r, 60)
	r = putU16(r, uint16(w))
	r = putU16(r, uint16(h))
	r = putU16(r, 0) // border
	r = putU16(r, 1) // class InputOutput
	r = putU32(r, 0) // visual CopyFromParent
	r = putU32(r, 0x802)
	r = putU32(r, x.blackPixel) // CWBackPixel
	r = putU32(r, 0x28003)      // CWEventMask: KeyPress|KeyRelease|Exposure|StructureNotify
	x.send(r)
	// name the window (XA_WM_NAME = 39, XA_STRING = 31)
	x.changeProperty(win, 39, 31, []byte(title))
	// CreateGC
	g := reqHeader(55, 0, 4)
	g = putU32(g, gc)
	g = putU32(g, win)
	g = putU32(g, 0)
	x.send(g)
	// MapWindow
	m := reqHeader(8, 0, 2)
	m = putU32(m, win)
	x.send(m)
	return win, gc, nil
}

func (x *xConn) changeProperty(win, prop, typ uint32, data []byte) {
	r := reqHeader(18, 0, uint16(6+(len(data)+3)/4))
	r = putU32(r, win)
	r = putU32(r, prop)
	r = putU32(r, typ)
	r = append(r, 8) // format 8-bit
	r = append(r, 0, 0, 0)
	r = putU32(r, uint32(len(data)))
	padded := make([]byte, (len(data)+3)&^3)
	copy(padded, data)
	r = append(r, padded...)
	x.send(r)
}

func (x *xConn) internAtom(name string) uint32 {
	r := reqHeader(16, 0, uint16(2+(len(name)+3)/4))
	r = putU16(r, uint16(len(name)))
	r = putU16(r, 0)
	padded := make([]byte, (len(name)+3)&^3)
	copy(padded, name)
	r = append(r, padded...)
	rep := x.sendForReply(r)
	if len(rep) >= 12 {
		return binary.LittleEndian.Uint32(rep[8:])
	}
	return 0
}

func (x *xConn) setWMDelete(win uint32) (uint32, uint32) {
	protocols := x.internAtom("WM_PROTOCOLS")
	del := x.internAtom("WM_DELETE_WINDOW")
	if protocols == 0 || del == 0 {
		return protocols, del
	}
	r := reqHeader(18, 0, 7)
	r = putU32(r, win)
	r = putU32(r, protocols)
	r = putU32(r, 4) // XA_ATOM
	r = append(r, 32)
	r = append(r, 0, 0, 0)
	r = putU32(r, 1)
	r = putU32(r, del)
	x.send(r)
	return protocols, del
}

// keyboardMapping resolves keysyms to keycodes.
func (x *xConn) keyboardMapping() map[uint32]byte {
	first := x.minKey
	count := int(x.maxKey) - int(first) + 1
	r := reqHeader(101, first, 2)
	r = append(r, byte(count), 0, 0, 0)
	rep := x.sendForReply(r)
	out := map[uint32]byte{}
	if len(rep) < 32 {
		return out
	}
	per := int(rep[1])
	for i := 0; i < count; i++ {
		for j := 0; j < per; j++ {
			off := 32 + (i*per+j)*4
			if off+4 <= len(rep) {
				sym := binary.LittleEndian.Uint32(rep[off:])
				if sym != 0 {
					if _, taken := out[sym]; !taken {
						out[sym] = first + byte(i)
					}
				}
			}
		}
	}
	return out
}

// putImageScaled paints the 320x240 framebuffer scaled xN into the window.
func (x *xConn) putImageScaled(win, gc uint32, fb []uint16, scale int) {
	vis, ok := x.visuals[x.rootVisual]
	if !ok {
		vis = xVisual{redMask: 0xff0000, greenMask: 0xff00, blueMask: 0xff}
	}
	rsh := shiftFor(vis.redMask)
	gsh := shiftFor(vis.greenMask)
	bsh := shiftFor(vis.blueMask)
	winW := scrW * scale
	winH := scrH * scale
	rowBytes := winW * 4
	maxData := (x.maxReqLen - 6) * 4
	if maxData < rowBytes {
		maxData = rowBytes
	}
	rowsPerBand := maxData / rowBytes
	if rowsPerBand > winH {
		rowsPerBand = winH
	}
	if rowsPerBand < 1 {
		rowsPerBand = 1
	}
	buf := make([]byte, rowsPerBand*rowBytes)
	for bandY := 0; bandY < winH; bandY += rowsPerBand {
		rows := rowsPerBand
		if bandY+rows > winH {
			rows = winH - bandY
		}
		data := buf[:rows*rowBytes]
		for ry := 0; ry < rows; ry++ {
			srcY := (bandY + ry) / scale
			for rx := 0; rx < winW; rx++ {
				c := fb[srcY*scrW+rx/scale]
				r8 := uint32(engRed(c) * 255 / 31)
				g8 := uint32(engGrn(c) * 255 / 31)
				b8 := uint32(engBlu(c) * 255 / 31)
				v := (r8 << rsh) | (g8 << gsh) | (b8 << bsh)
				off := ry*rowBytes + rx*4
				if x.imgLSB {
					binary.LittleEndian.PutUint32(data[off:], v)
				} else {
					binary.BigEndian.PutUint32(data[off:], v)
				}
			}
		}
		r := reqHeader(72, 2, uint16(6+len(data)/4))
		r = putU32(r, win)
		r = putU32(r, gc)
		r = putU16(r, uint16(winW))
		r = putU16(r, uint16(rows))
		r = putI16(r, 0)
		r = putI16(r, int16(bandY))
		r = append(r, 0)
		r = append(r, 24)
		r = putU16(r, 0)
		r = append(r, data...)
		x.send(r)
	}
}

func shiftFor(mask uint32) uint {
	s := uint(0)
	for mask != 0 && mask&1 == 0 {
		mask >>= 1
		s++
	}
	return s
}

// getImage reads back the window contents (for the self-check).
func (x *xConn) getImage(win uint32, w, h int) []byte {
	r := reqHeader(73, 2, 5)
	r = putU32(r, win)
	r = putI16(r, 0)
	r = putI16(r, 0)
	r = putU16(r, uint16(w))
	r = putU16(r, uint16(h))
	r = putU32(r, 0xFFFFFFFF)
	rep := x.sendForReply(r)
	if len(rep) <= 32 {
		return nil
	}
	return rep[32:]
}
