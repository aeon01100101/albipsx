package main

// ============================================================
//  selftest.go - proof that the z-buffer really works.
//  Run: albipsx --selftest
// ============================================================

import (
	"fmt"
	"math"
)

func runSelfTest() int {
	pass, fail := 0, 0
	check := func(name string, ok bool) {
		if ok {
			pass++
			fmt.Println("PASS " + name)
		} else {
			fail++
			fmt.Println("FAIL " + name)
		}
	}

	red := engPack(31, 0, 0)
	green := engPack(0, 31, 0)

	mkTri := func(z int32, col uint16) *engTri {
		return &engTri{
			x0: 20, y0: 20, x1: 300, y1: 40, x2: 160, y2: 220,
			cz0: z, cz1: z, cz2: z, col: col, tex: -1, shade: 256,
			s0: 256, s1: 256, s2: 256,
		}
	}

	// 1. the z-buffer exists and is the screen's size
	e := newEngine()
	check("z-buffer exists (320x240 depths)", len(e.ZB) == scrPix)
	check("framebuffer exists", len(e.FB) == scrPix)

	// 2. draw order does not matter: far red, then near green
	e.clearSky()
	e.rasterFlat(mkTri(9000, red))
	e.rasterFlat(mkTri(5000, green))
	check("near triangle wins when drawn last", e.FB[100*scrW+160] == green)
	check("z-buffer stored the near depth", e.ZB[100*scrW+160] == 5000)
	first := make([]uint16, scrPix)
	copy(first, e.FB)

	// 3. ...and near green first, then far red: same picture
	e.clearSky()
	e.rasterFlat(mkTri(5000, green))
	e.rasterFlat(mkTri(9000, red))
	check("near triangle wins when drawn first", e.FB[100*scrW+160] == green)
	same := true
	for i := range first {
		if first[i] != e.FB[i] {
			same = false
			break
		}
	}
	check("both draw orders give the identical picture", same)

	// 4. crossing depth ramps: two big triangles cover the
	// middle together; red gets deeper left-to-right, green
	// gets shallower, so each side is won by a different one.
	e.clearSky()
	rampR := &engTri{x0: 20, y0: 20, x1: 300, y1: 20, x2: 160, y2: 230,
		cz0: 3000, cz1: 13000, cz2: 8000, col: red, tex: -1, shade: 256, s0: 256, s1: 256, s2: 256}
	rampG := &engTri{x0: 20, y0: 230, x1: 300, y1: 230, x2: 160, y2: 10,
		cz0: 13000, cz1: 3000, cz2: 8000, col: green, tex: -1, shade: 256, s0: 256, s1: 256, s2: 256}
	e.rasterFlat(rampR)
	e.rasterFlat(rampG)
	lpix := e.FB[120*scrW+100]
	rpix := e.FB[120*scrW+220]
	check("crossing ramps: left pixel is the nearer red", lpix == red)
	check("crossing ramps: right pixel is the nearer green", rpix == green)

	// 5. depth clears between frames (no ghosting)
	e.clearSky()
	check("z-buffer resets to far", e.ZB[100*scrW+160] == math.MaxInt32)
	e.rasterFlat(mkTri(9000, red))
	check("far triangle shows again after clear", e.FB[100*scrW+160] == red)

	// 6. a whole game scene renders the same twice (deterministic)
	build := func() *World {
		w := newWorld()
		w.Start("GEM HUNT", true)
		w.MakeSky("sky")
		w.MakeGround("grass")
		w.AddTree(-4, 3)
		w.AddHouse(6, 8, "orange")
		w.AddMountain(8, -7, "gray")
		w.AddBox(2, 2, "blue")
		w.AddGem(0, -2)
		w.AddGem(3, 3)
		w.PutPlayer(0, -6, "red")
		return w
	}
	w1 := build()
	w1.DrawFrame()
	c1 := w1.Eng.fbChecksum()
	w2 := build()
	w2.DrawFrame()
	c2 := w2.Eng.fbChecksum()
	check("game scene renders deterministically", c1 == c2 && c1 != 0)
	check("game scene draws real triangles", w1.Eng.TrisDrawn > 50)

	// 7. the floor no longer eats the hero (the old painter
	// bug): a healthy patch of hero-red pixels must survive
	// in the middle of the picture, where the hero stands.
	sx, sy, _, ok := w1.Eng.projectPoint(w1.PX, wU/2, w1.PZ)
	check("hero projects onto the screen", ok && sx > 100 && sx < 220 && sy > 60 && sy < 239)
	heroRed := 0
	for y := 80; y < 240; y++ {
		for x := 100; x < 220; x++ {
			c := w1.Eng.FB[y*scrW+x]
			if engRed(c) > 15 && engRed(c) > engGrn(c)+8 && engRed(c) > engBlu(c)+8 {
				heroRed++
			}
		}
	}
	check("hero body is hero coloured (z-buffer keeps it whole)", heroRed > 40)

	// 8. the budget, in the open
	fmt.Printf("BUDGET framebuffer %d KB, z-buffer %d KB, textures %d KB, total engine buffers %d KB (rule: under %d KB)\n",
		scrPix*2/1024, scrPix*4/1024, len(e.Tex)*1024*2/1024,
		(scrPix*2+scrPix*4+len(e.Tex)*2048)/1024, 250*1024)
	check("engine buffers far under 250 MB", scrPix*2+scrPix*4+len(e.Tex)*2048 < 250*1024*1024)

	fmt.Printf("SELFTEST PASS %d FAIL %d\n", pass, fail)
	if fail > 0 {
		return 1
	}
	return 0
}
