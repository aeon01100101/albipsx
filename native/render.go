package main

// ============================================================
//  AlbiPSX native renderer - PS1-style graphics WITH a
//  Z-buffer (per the boss's orders: yes, we need a z buffer).
//
//  Kept PS1: 320x240, 15-bit colour, fixed-point 3D maths,
//  vertex-snap wobble, affine (warpy) textures, banded light,
//  tiny 32x32 textures, painter-era triangle clipping.
//
//  New vs the Albish engine: every pixel now carries a depth
//  (camera Z, fixed point, 4 bytes). A pixel is only painted
//  when its triangle is nearer than what's already there.
//  Draw order no longer matters: the floor can't eat the car,
//  and crossing walls sort themselves out per pixel.
//
//  RAM: framebuffer 150 KB + z-buffer 300 KB + textures
//  ~12 KB. Still a rounding error under the 250 MB rule.
// ============================================================

import (
	"image"
	"image/png"
	"math"
	"math/rand"
	"os"
)

const (
	scrW   = 320
	scrH   = 240
	scrPix = 76800
	fixOne = 4096
	focal  = 230
	nearZ  = 1200
	snapQ  = 24
	maxTri = 600
	maxPrt = 64
)

func engClamp(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func engPack(r, g, b int32) uint16 {
	return uint16(engClamp(r, 0, 31)*1024 + engClamp(g, 0, 31)*32 + engClamp(b, 0, 31))
}

func engRed(c uint16) int32 { return int32((c / 1024) % 32) }
func engGrn(c uint16) int32 { return int32((c / 32) % 32) }
func engBlu(c uint16) int32 { return int32(c % 32) }

func engShade(c uint16, s int32) uint16 {
	if s >= 256 {
		return c
	}
	if s <= 0 {
		return 0
	}
	return engPack(engRed(c)*s/256, engGrn(c)*s/256, engBlu(c)*s/256)
}

func engMix(c1, c2 uint16, t int32) uint16 {
	return engPack(
		engRed(c1)+(engRed(c2)-engRed(c1))*t/256,
		engGrn(c1)+(engGrn(c2)-engGrn(c1))*t/256,
		engBlu(c1)+(engBlu(c2)-engBlu(c1))*t/256)
}

func engFmul(a, b int32) int32 { return int32((int64(a) * int64(b)) / fixOne) }

func engEdge(ax, ay, bx, by, px, py int32) int64 {
	return int64(px-ax)*int64(by-ay) - int64(py-ay)*int64(bx-ax)
}

type engTri struct {
	x0, y0, x1, y1, x2, y2 int32
	cz0, cz1, cz2          int32
	col                    uint16
	tex                    int32
	shade                  int32
	mode                   int32 // 0 flat, 1 gouraud, 2 textured
	s0, s1, s2             int32
	u0, v0, u1, v1, u2, v2 int32
}

type engMesh struct {
	verts  [][3]int32
	faces  [][5]int32 // a, b, c, colour, tex
	uv     [][2]int32
	vsh    []int32
	ground bool
}

type engThing struct {
	mesh    *engMesh
	x, y, z int32
	ry      int32
}

type engPart struct {
	x, y, z    int32
	vx, vy, vz int32
	life       int32
	col        uint16
}

type Engine struct {
	FB       []uint16
	ZB       []int32
	Tex      [][]uint16
	texShade map[int32][]uint16
	Sin      [256]int32
	Cos      [256]int32
	SkyTop   uint16
	SkyBot   uint16

	camX, camY, camZ       int32
	camCos, camSin         int32
	pitCos, pitSin         int32
	lx, ly, lz             int32
	Tick                   int
	TrisSeen, TrisDrawn    int
	Pixels                 int
	Rnd                    *rand.Rand
	parts                  []engPart
	ready                  bool
}

func newEngine() *Engine {
	e := &Engine{
		FB:       make([]uint16, scrPix),
		ZB:       make([]int32, scrPix),
		texShade: map[int32][]uint16{},
		Rnd:      rand.New(rand.NewSource(12345)),
	}
	for i := 0; i < 256; i++ {
		e.Sin[i] = int32(math.Sin(float64(i)*6.28318530718/256.0) * 4096.0)
		e.Cos[i] = int32(math.Cos(float64(i)*6.28318530718/256.0) * 4096.0)
	}
	e.buildTextures()
	e.SkyTop = engPack(6, 14, 29)
	e.SkyBot = engPack(20, 26, 31)
	e.ready = true
	return e
}

func (e *Engine) sinA(a int32) int32 { return e.Sin[((a%256)+256)%256] }
func (e *Engine) cosA(a int32) int32 { return e.Cos[((a%256)+256)%256] }

func (e *Engine) buildTextures() {
	mk := func(fn func(x, y int32) uint16) {
		t := make([]uint16, 1024)
		for y := int32(0); y < 32; y++ {
			for x := int32(0); x < 32; x++ {
				t[y*32+x] = fn(x, y)
			}
		}
		e.Tex = append(e.Tex, t)
	}
	mk(func(x, y int32) uint16 { // 0 grass checker
		if (x/8+y/8)%2 == 0 {
			return engPack(7, 23, 7)
		}
		return engPack(5, 18, 5)
	})
	mk(func(x, y int32) uint16 { // 1 stone checker
		if (x/8+y/8)%2 == 0 {
			return engPack(17, 16, 19)
		}
		return engPack(12, 11, 15)
	})
	mk(func(x, y int32) uint16 { // 2 bricks
		row := y / 8
		off := int32(0)
		if row%2 == 1 {
			off = 8
		}
		if y%8 == 7 || (x+off)%16 == 15 {
			return engPack(20, 18, 16)
		}
		return engPack(24, 9, 6)
	})
	mk(func(x, y int32) uint16 { // 3 wood
		if x%8 == 7 {
			return engPack(10, 6, 3)
		}
		if (x/8)%2 == 0 {
			return engPack(19, 12, 5)
		}
		return engPack(17, 10, 4)
	})
	mk(func(x, y int32) uint16 { // 4 party dots
		dx := (x % 16) - 8
		dy := (y % 16) - 8
		if dx*dx+dy*dy < 12 {
			return engPack(31, 28, 8)
		}
		return engPack(9, 7, 22)
	})
	mk(func(x, y int32) uint16 { // 5 sand checker
		if (x/8+y/8)%2 == 0 {
			return engPack(29, 24, 13)
		}
		return engPack(26, 21, 10)
	})
}

func (e *Engine) setSky(top, bot uint16) { e.SkyTop, e.SkyBot = top, bot }

func (e *Engine) setCamera(x, y, z, yaw, pitch int32) {
	e.camX, e.camY, e.camZ = x, y, z
	e.camCos, e.camSin = e.cosA(yaw), e.sinA(yaw)
	e.pitCos, e.pitSin = e.cosA(pitch), e.sinA(pitch)
	lx, ly, lz := int32(-1024), int32(3400), int32(1800)
	rx := engFmul(lx, e.camCos) - engFmul(lz, e.camSin)
	rz := engFmul(lx, e.camSin) + engFmul(lz, e.camCos)
	ry := engFmul(ly, e.pitCos) - engFmul(rz, e.pitSin)
	rz2 := engFmul(ly, e.pitSin) + engFmul(rz, e.pitCos)
	e.lx, e.ly, e.lz = rx, ry, rz2
}

func (e *Engine) clearSky() {
	bands := int32(16)
	bh := int32(scrH) / bands
	for band := int32(0); band < bands; band++ {
		col := engMix(e.SkyTop, e.SkyBot, band*256/(bands-1))
		for y := band * bh; y < (band+1)*bh; y++ {
			base := y * scrW
			for x := int32(0); x < scrW; x++ {
				e.FB[base+x] = col
			}
		}
	}
	for i := range e.ZB {
		e.ZB[i] = math.MaxInt32
	}
}

// fillRectZ paints a rect only where it wins the depth test
// (used for sparkles: they sparkle on top of what's nearer,
// never hide what's nearer, and never write depth).
func (e *Engine) fillRectZ(x0, y0, x1, y1 int32, z int32, col uint16) {
	x0 = engClamp(x0, 0, scrW-1)
	x1 = engClamp(x1, 0, scrW-1)
	y0 = engClamp(y0, 0, scrH-1)
	y1 = engClamp(y1, 0, scrH-1)
	for y := y0; y <= y1; y++ {
		base := y * scrW
		for x := x0; x <= x1; x++ {
			if z < e.ZB[base+x] {
				e.FB[base+x] = col
			}
		}
	}
}

func (e *Engine) fillRect(x0, y0, x1, y1 int32, col uint16) {
	x0 = engClamp(x0, 0, scrW-1)
	x1 = engClamp(x1, 0, scrW-1)
	y0 = engClamp(y0, 0, scrH-1)
	y1 = engClamp(y1, 0, scrH-1)
	for y := y0; y <= y1; y++ {
		base := y * scrW
		for x := x0; x <= x1; x++ {
			e.FB[base+x] = col
		}
	}
}

// ---------- triangle painters, now with depth ----------
// Same stepping walkers as the Albish engine, plus one more
// interpolated value: depth. z = znum / area, tested against
// the z-buffer before any painting happens.

type triPrep struct {
	area                                   int64
	minx, maxx, miny, maxy                 int32
	w0, w1, w2                             int64
	dwdx0, dwdy0, dwdx1, dwdy1, dwdx2, dwdy2 int64
	znum, dzdx, dzdy                       int64
	ok                                     bool
}

func (e *Engine) prepTri(t *engTri) triPrep {
	p := triPrep{}
	area := engEdge(t.x0, t.y0, t.x1, t.y1, t.x2, t.y2)
	if area == 0 {
		return p
	}
	p.minx = engClamp(min32(t.x0, min32(t.x1, t.x2)), 0, scrW-1)
	p.maxx = engClamp(max32(t.x0, max32(t.x1, t.x2)), 0, scrW-1)
	p.miny = engClamp(min32(t.y0, min32(t.y1, t.y2)), 0, scrH-1)
	p.maxy = engClamp(max32(t.y0, max32(t.y1, t.y2)), 0, scrH-1)
	p.dwdx0 = int64(t.y2 - t.y1)
	p.dwdy0 = int64(t.x1 - t.x2)
	p.dwdx1 = int64(t.y0 - t.y2)
	p.dwdy1 = int64(t.x2 - t.x0)
	p.dwdx2 = int64(t.y1 - t.y0)
	p.dwdy2 = int64(t.x0 - t.x1)
	p.w0 = engEdge(t.x1, t.y1, t.x2, t.y2, p.minx, p.miny)
	p.w1 = engEdge(t.x2, t.y2, t.x0, t.y0, p.minx, p.miny)
	p.w2 = engEdge(t.x0, t.y0, t.x1, t.y1, p.minx, p.miny)
	if area < 0 {
		area = -area
		p.w0, p.w1, p.w2 = -p.w0, -p.w1, -p.w2
		p.dwdx0, p.dwdy0 = -p.dwdx0, -p.dwdy0
		p.dwdx1, p.dwdy1 = -p.dwdx1, -p.dwdy1
		p.dwdx2, p.dwdy2 = -p.dwdx2, -p.dwdy2
	}
	p.area = area
	p.znum = p.w0*int64(t.cz0) + p.w1*int64(t.cz1) + p.w2*int64(t.cz2)
	p.dzdx = int64(t.cz0)*p.dwdx0 + int64(t.cz1)*p.dwdx1 + int64(t.cz2)*p.dwdx2
	p.dzdy = int64(t.cz0)*p.dwdy0 + int64(t.cz1)*p.dwdy1 + int64(t.cz2)*p.dwdy2
	p.ok = true
	return p
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func (e *Engine) rasterFlat(t *engTri) int {
	p := e.prepTri(t)
	if !p.ok {
		return 0
	}
	count := 0
	w0, w1, w2, znum := p.w0, p.w1, p.w2, p.znum
	for y := p.miny; y <= p.maxy; y++ {
		w0r, w1r, w2r, zrow := w0, w1, w2, znum
		base := y * scrW
		for x := p.minx; x <= p.maxx; x++ {
			if w0r >= 0 && w1r >= 0 && w2r >= 0 {
				z := int32(zrow / p.area)
				idx := base + x
				if z < e.ZB[idx] {
					e.ZB[idx] = z
					e.FB[idx] = t.col
					count++
				}
			}
			w0r += p.dwdx0
			w1r += p.dwdx1
			w2r += p.dwdx2
			zrow += p.dzdx
		}
		w0 += p.dwdy0
		w1 += p.dwdy1
		w2 += p.dwdy2
		znum += p.dzdy
	}
	return count
}

func (e *Engine) rasterGouraud(t *engTri) int {
	p := e.prepTri(t)
	if !p.ok {
		return 0
	}
	snum := p.w0*int64(t.s0) + p.w1*int64(t.s1) + p.w2*int64(t.s2)
	dsdx := int64(t.s0)*p.dwdx0 + int64(t.s1)*p.dwdx1 + int64(t.s2)*p.dwdx2
	dsdy := int64(t.s0)*p.dwdy0 + int64(t.s1)*p.dwdy1 + int64(t.s2)*p.dwdy2
	count := 0
	w0, w1, w2, znum := p.w0, p.w1, p.w2, p.znum
	for y := p.miny; y <= p.maxy; y++ {
		w0r, w1r, w2r, zrow, srow := w0, w1, w2, znum, snum
		base := y * scrW
		for x := p.minx; x <= p.maxx; x++ {
			if w0r >= 0 && w1r >= 0 && w2r >= 0 {
				z := int32(zrow / p.area)
				idx := base + x
				if z < e.ZB[idx] {
					e.ZB[idx] = z
					e.FB[idx] = engShade(t.col, engClamp(int32(srow/p.area), 0, 256))
					count++
				}
			}
			w0r += p.dwdx0
			w1r += p.dwdx1
			w2r += p.dwdx2
			zrow += p.dzdx
			srow += dsdx
		}
		w0 += p.dwdy0
		w1 += p.dwdy1
		w2 += p.dwdy2
		znum += p.dzdy
		snum += dsdy
	}
	return count
}

func (e *Engine) rasterTex(t *engTri) int {
	p := e.prepTri(t)
	if !p.ok {
		return 0
	}
	unum := p.w0*int64(t.u0) + p.w1*int64(t.u1) + p.w2*int64(t.u2)
	dudx := int64(t.u0)*p.dwdx0 + int64(t.u1)*p.dwdx1 + int64(t.u2)*p.dwdx2
	dudy := int64(t.u0)*p.dwdy0 + int64(t.u1)*p.dwdy1 + int64(t.u2)*p.dwdy2
	vnum := p.w0*int64(t.v0) + p.w1*int64(t.v1) + p.w2*int64(t.v2)
	dvdx := int64(t.v0)*p.dwdx0 + int64(t.v1)*p.dwdx1 + int64(t.v2)*p.dwdx2
	dvdy := int64(t.v0)*p.dwdy0 + int64(t.v1)*p.dwdy1 + int64(t.v2)*p.dwdy2
	tex := e.Tex[t.tex]
	if t.shade != 256 {
		key := t.tex*512 + t.shade
		if shaded, ok := e.texShade[key]; ok {
			tex = shaded
		} else {
			shaded := make([]uint16, 1024)
			for i, c := range e.Tex[t.tex] {
				shaded[i] = engShade(c, t.shade)
			}
			e.texShade[key] = shaded
			tex = shaded
		}
	}
	count := 0
	w0, w1, w2, znum := p.w0, p.w1, p.w2, p.znum
	for y := p.miny; y <= p.maxy; y++ {
		w0r, w1r, w2r, zrow, urow, vrow := w0, w1, w2, znum, unum, vnum
		base := y * scrW
		for x := p.minx; x <= p.maxx; x++ {
			if w0r >= 0 && w1r >= 0 && w2r >= 0 {
				z := int32(zrow / p.area)
				idx := base + x
				if z < e.ZB[idx] {
					e.ZB[idx] = z
					uu := urow / p.area
					vv := vrow / p.area
					tx := (uu / 128) % 32
					ty := (vv / 128) % 32
					if tx < 0 {
						tx += 32
					}
					if ty < 0 {
						ty += 32
					}
					e.FB[idx] = tex[ty*32+tx]
					count++
				}
			}
			w0r += p.dwdx0
			w1r += p.dwdx1
			w2r += p.dwdx2
			zrow += p.dzdx
			urow += dudx
			vrow += dvdx
		}
		w0 += p.dwdy0
		w1 += p.dwdy1
		w2 += p.dwdy2
		znum += p.dzdy
		unum += dudy
		vnum += dvdy
	}
	return count
}

// ---------- shapes ----------
func engMeshBox(hx, hy, hz int32, cols [6]uint16) *engMesh {
	m := &engMesh{}
	m.verts = [][3]int32{
		{-hx, -hy, -hz}, {hx, -hy, -hz}, {hx, hy, -hz}, {-hx, hy, -hz},
		{-hx, -hy, hz}, {hx, -hy, hz}, {hx, hy, hz}, {-hx, hy, hz},
	}
	f := [][4]int32{{4, 5, 6, 0}, {4, 6, 7, 0}, {1, 0, 3, 1}, {1, 3, 2, 1},
		{0, 4, 7, 2}, {0, 7, 3, 2}, {5, 1, 2, 3}, {5, 2, 6, 3},
		{7, 6, 2, 4}, {7, 2, 3, 4}, {0, 1, 5, 5}, {0, 5, 4, 5}}
	for _, q := range f {
		m.faces = append(m.faces, [5]int32{q[0], q[1], q[2], int32(cols[q[3]]), -1})
	}
	return m
}

func engMeshPyramid(hx, hz, hgt int32, cols [5]uint16) *engMesh {
	m := &engMesh{}
	m.verts = [][3]int32{{-hx, 0, -hz}, {hx, 0, -hz}, {hx, 0, hz}, {-hx, 0, hz}, {0, hgt, 0}}
	f := [][4]int32{{3, 2, 4, 0}, {2, 1, 4, 1}, {1, 0, 4, 2}, {0, 3, 4, 3}, {0, 1, 2, 4}, {0, 2, 3, 4}}
	for _, q := range f {
		m.faces = append(m.faces, [5]int32{q[0], q[1], q[2], int32(cols[q[3]]), -1})
	}
	return m
}

func engMeshGem(r, hgt int32, col uint16) *engMesh {
	m := &engMesh{}
	m.verts = [][3]int32{{0, hgt, 0}, {r, 0, 0}, {0, 0, r}, {-r, 0, 0}, {0, 0, -r}, {0, -hgt, 0}}
	f := [][3]int32{{0, 2, 1}, {0, 3, 2}, {0, 4, 3}, {0, 1, 4}, {5, 1, 2}, {5, 2, 3}, {5, 3, 4}, {5, 4, 1}}
	for _, q := range f {
		m.faces = append(m.faces, [5]int32{q[0], q[1], q[2], int32(col), -1})
	}
	m.vsh = []int32{256, 205, 205, 205, 205, 95}
	return m
}

func engMeshGround(half int32, texid int32, col uint16) *engMesh {
	m := &engMesh{ground: true}
	cells := int32(8)
	step := (half * 2) / cells
	for iz := int32(0); iz <= cells; iz++ {
		for ix := int32(0); ix <= cells; ix++ {
			m.verts = append(m.verts, [3]int32{-half + ix*step, 0, -half + iz*step})
			m.uv = append(m.uv, [2]int32{ix * 2048, iz * 2048})
		}
	}
	for iz := int32(0); iz < cells; iz++ {
		for ix := int32(0); ix < cells; ix++ {
			a := iz*(cells+1) + ix
			b := a + 1
			d := a + cells + 1
			c := d + 1
			m.faces = append(m.faces, [5]int32{a, c, b, int32(col), texid})
			m.faces = append(m.faces, [5]int32{a, d, c, int32(col), texid})
		}
	}
	return m
}

// ---------- 3D pipeline: world -> camera -> screen ----------
func (e *Engine) projectPoint(wx, wy, wz int32) (int32, int32, int32, bool) {
	dx := wx - e.camX
	dy := wy - e.camY
	dz := wz - e.camZ
	cx := engFmul(dx, e.camCos) - engFmul(dz, e.camSin)
	cz := engFmul(dx, e.camSin) + engFmul(dz, e.camCos)
	cy := engFmul(dy, e.pitCos) - engFmul(cz, e.pitSin)
	cz2 := engFmul(dy, e.pitSin) + engFmul(cz, e.pitCos)
	cx = (cx / snapQ) * snapQ
	cy = (cy / snapQ) * snapQ
	cz2 = (cz2 / snapQ) * snapQ
	if cz2 < nearZ {
		return 0, 0, 0, false
	}
	return 160 + (cx*focal)/cz2, 120 - (cy*focal)/cz2, cz2, true
}

func (e *Engine) renderScene(things []engThing) int {
	e.TrisSeen = 0
	e.TrisDrawn = 0
	e.Pixels = 0
	e.texShade = map[int32][]uint16{}
	tris := make([]engTri, 0, 256)
	for _, th := range things {
		tris = e.addThingTris(tris, th)
		if len(tris) >= maxTri {
			break
		}
	}
	e.clearSky()
	for i := range tris {
		t := &tris[i]
		switch t.mode {
		case 2:
			e.Pixels += e.rasterTex(t)
		case 1:
			e.Pixels += e.rasterGouraud(t)
		default:
			e.Pixels += e.rasterFlat(t)
		}
		e.TrisDrawn++
	}
	e.drawParts()
	e.Tick++
	return e.TrisDrawn
}

func (e *Engine) addThingTris(tris []engTri, th engThing) []engTri {
	mesh := th.mesh
	ca := e.cosA(th.ry)
	sa := e.sinA(th.ry)
	n := len(mesh.verts)
	tvx := make([]int32, n)
	tvy := make([]int32, n)
	tvz := make([]int32, n)
	tsx := make([]int32, n)
	tsy := make([]int32, n)
	tok := make([]bool, n)
	for i, v := range mesh.verts {
		rx := engFmul(v[0], ca) - engFmul(v[2], sa)
		rz := engFmul(v[0], sa) + engFmul(v[2], ca)
		wx := rx + th.x
		wy := v[1] + th.y
		wz := rz + th.z
		dx := wx - e.camX
		dy := wy - e.camY
		dz := wz - e.camZ
		cx := engFmul(dx, e.camCos) - engFmul(dz, e.camSin)
		cz := engFmul(dx, e.camSin) + engFmul(dz, e.camCos)
		cy := engFmul(dy, e.pitCos) - engFmul(cz, e.pitSin)
		cz2 := engFmul(dy, e.pitSin) + engFmul(cz, e.pitCos)
		cx = (cx / snapQ) * snapQ
		cy = (cy / snapQ) * snapQ
		cz2 = (cz2 / snapQ) * snapQ
		tvx[i], tvy[i], tvz[i] = cx, cy, cz2
		if cz2 < nearZ {
			tok[i] = false
		} else {
			tsx[i] = 160 + (cx*focal)/cz2
			tsy[i] = 120 - (cy*focal)/cz2
			tok[i] = true
		}
	}
	for _, f := range mesh.faces {
		e.TrisSeen++
		a, b, c := f[0], f[1], f[2]
		if !tok[a] || !tok[b] || !tok[c] {
			continue
		}
		ux := tvx[b] - tvx[a]
		uy := tvy[b] - tvy[a]
		uz := tvz[b] - tvz[a]
		vx := tvx[c] - tvx[a]
		vy := tvy[c] - tvy[a]
		vz := tvz[c] - tvz[a]
		nx := int64(uy)*int64(vz) - int64(uz)*int64(vy)
		ny := int64(uz)*int64(vx) - int64(ux)*int64(vz)
		nz := int64(ux)*int64(vy) - int64(uy)*int64(vx)
		mx := (tvx[a] + tvx[b] + tvx[c]) / 3
		my := (tvy[a] + tvy[b] + tvy[c]) / 3
		mz := (tvz[a] + tvz[b] + tvz[c]) / 3
		facing := nx*int64(mx) + ny*int64(my) + nz*int64(mz)
		if facing >= 0 {
			continue
		}
		nlen := int64(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
		shade := int32(176)
		if nlen > 0 {
			ndot := (nx*int64(e.lx) + ny*int64(e.ly) + nz*int64(e.lz)) * 4096 / (nlen * 3981)
			shade = 176 + int32(ndot*80/4096)
		}
		shade = engClamp(shade, 96, 256)
		shade = (shade / 16) * 16
		t := engTri{
			x0: tsx[a], y0: tsy[a], x1: tsx[b], y1: tsy[b], x2: tsx[c], y2: tsy[c],
			cz0: tvz[a], cz1: tvz[b], cz2: tvz[c],
			col: uint16(f[3]), tex: f[4], shade: shade,
			s0: 256, s1: 256, s2: 256,
		}
		if f[4] >= 0 {
			t.mode = 2
			if mesh.uv != nil {
				t.u0, t.v0 = mesh.uv[a][0], mesh.uv[a][1]
				t.u1, t.v1 = mesh.uv[b][0], mesh.uv[b][1]
				t.u2, t.v2 = mesh.uv[c][0], mesh.uv[c][1]
			}
		} else if mesh.vsh != nil {
			t.mode = 1
			t.s0 = shade * mesh.vsh[a] / 256
			t.s1 = shade * mesh.vsh[b] / 256
			t.s2 = shade * mesh.vsh[c] / 256
		} else {
			t.col = engShade(uint16(f[3]), shade)
		}
		if len(tris) < maxTri {
			tris = append(tris, t)
		}
	}
	return tris
}

// ---------- sparkles ----------
func (e *Engine) spawnSparkle(x, y, z int32, col uint16, n int) {
	for i := 0; i < n && len(e.parts) < maxPrt; i++ {
		e.parts = append(e.parts, engPart{
			x: x, y: y, z: z,
			vx: int32(e.Rnd.Intn(641) - 320),
			vy: int32(500 + e.Rnd.Intn(1000)),
			vz: int32(e.Rnd.Intn(641) - 320),
			life: int32(18 + e.Rnd.Intn(18)),
			col:  col,
		})
	}
}

func (e *Engine) stepParts() {
	out := e.parts[:0]
	for _, p := range e.parts {
		p.x += p.vx / 3
		p.y += p.vy / 3
		p.z += p.vz / 3
		p.vy -= 30
		p.life--
		if p.life > 0 && p.y >= 0 {
			out = append(out, p)
		}
	}
	e.parts = out
}

func (e *Engine) drawParts() {
	for _, p := range e.parts {
		sx, sy, cz, ok := e.projectPoint(p.x, p.y, p.z)
		if !ok {
			continue
		}
		sz := engClamp((focal*260)/cz, 1, 7)
		half := sz / 2
		e.fillRectZ(sx-half, sy-half, sx+half, sy+half, cz, p.col)
	}
}

// ---------- tiny 3x5 letters ----------
var engGlyphs = map[rune][5]string{
	'A': {"010", "101", "111", "101", "101"}, 'B': {"110", "101", "110", "101", "110"},
	'C': {"011", "100", "100", "100", "011"}, 'D': {"110", "101", "101", "101", "110"},
	'E': {"111", "100", "110", "100", "111"}, 'F': {"111", "100", "110", "100", "100"},
	'G': {"011", "100", "101", "101", "011"}, 'H': {"101", "101", "111", "101", "101"},
	'I': {"111", "010", "010", "010", "111"}, 'J': {"001", "001", "001", "101", "010"},
	'K': {"101", "101", "110", "101", "101"}, 'L': {"100", "100", "100", "100", "111"},
	'M': {"101", "111", "101", "101", "101"}, 'N': {"101", "111", "101", "101", "101"},
	'O': {"010", "101", "101", "101", "010"}, 'P': {"110", "101", "110", "100", "100"},
	'Q': {"010", "101", "101", "010", "001"}, 'R': {"110", "101", "110", "101", "101"},
	'S': {"011", "100", "010", "001", "110"}, 'T': {"111", "010", "010", "010", "010"},
	'U': {"101", "101", "101", "101", "111"}, 'V': {"101", "101", "101", "101", "010"},
	'W': {"101", "101", "101", "111", "101"}, 'X': {"101", "101", "010", "101", "101"},
	'Y': {"101", "101", "010", "010", "010"}, 'Z': {"111", "001", "010", "100", "111"},
	'0': {"010", "101", "101", "101", "010"}, '1': {"010", "110", "010", "010", "111"},
	'2': {"110", "001", "010", "100", "111"}, '3': {"110", "001", "010", "001", "110"},
	'4': {"101", "101", "111", "001", "001"}, '5': {"111", "100", "110", "001", "110"},
	'6': {"011", "100", "110", "101", "010"}, '7': {"111", "001", "001", "010", "010"},
	'8': {"010", "101", "010", "101", "010"}, '9': {"010", "101", "011", "001", "110"},
	' ': {"000", "000", "000", "000", "000"}, '.': {"000", "000", "000", "000", "010"},
	'!': {"010", "010", "010", "000", "010"}, ':': {"000", "010", "000", "010", "000"},
	'-': {"000", "000", "111", "000", "000"}, '+': {"000", "010", "111", "010", "000"},
	'/': {"001", "001", "010", "100", "100"},
}

func (e *Engine) drawText(x, y int32, text string, col uint16, scale int32) {
	cx := x
	for _, ch := range text {
		if ch >= 'a' && ch <= 'z' {
			ch = ch - 'a' + 'A'
		}
		if g, ok := engGlyphs[ch]; ok {
			for row := int32(0); row < 5; row++ {
				bits := g[row]
				for ci := int32(0); ci < 3; ci++ {
					if bits[ci] == '1' {
						e.fillRect(cx+ci*scale, y+row*scale, cx+ci*scale+scale-1, y+row*scale+scale-1, col)
					}
				}
			}
		}
		cx += 4 * scale
	}
}

func (e *Engine) fbChecksum() int64 {
	var total int64
	for i := 0; i < scrPix; i += 997 {
		total += int64(e.FB[i])
	}
	return total
}

// savePNG writes the framebuffer as a PNG (5-bit channels scaled to 8-bit).
func (e *Engine) savePNG(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, scrW, scrH))
	for y := 0; y < scrH; y++ {
		for x := 0; x < scrW; x++ {
			c := e.FB[y*scrW+x]
			i := img.PixOffset(x, y)
			img.Pix[i+0] = uint8(engRed(c) * 255 / 31)
			img.Pix[i+1] = uint8(engGrn(c) * 255 / 31)
			img.Pix[i+2] = uint8(engBlu(c) * 255 / 31)
			img.Pix[i+3] = 255
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
