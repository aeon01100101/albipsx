package main

// ============================================================
//  world.go - the kindergarten world, in native code.
//  Same friendly words as easy.alb, same colours, same rules,
//  but running at full speed inside the Linux program.
//  Games written in Albish (game.alb, my_first_game.alb)
//  build their world by calling these through the embedded
//  Albish interpreter - the script doesn't know or care
//  that the words are native now.
// ============================================================

import (
	"fmt"
	"math"
	"strings"
)

const wU = 4096

type wItem struct {
	thing engThing
	kind  string
	got   bool
	baseY int32
}

type World struct {
	Eng       *Engine
	Title     string
	Items     []*wItem
	HeroParts []*wItem
	GemsTotal int
	GemsGot   int
	PX, PZ    int32
	DirF      float64 // facing, in brads (256 = a full turn)
	Msg       string
	Frame     int
	Won       bool
	HasPlayer bool
	bonkCool  int
	avoid     int
}

func newWorld() *World {
	return &World{Eng: newEngine(), Title: "MY GAME"}
}

func (w *World) Dir() int32 {
	d := int32(math.Round(w.DirF))
	return ((d % 256) + 256) % 256
}

var wColors = map[string]uint16{}

func init() {
	p := engPack
	wColors["red"] = p(31, 5, 5)
	wColors["orange"] = p(31, 17, 3)
	wColors["yellow"] = p(31, 29, 6)
	wColors["green"] = p(7, 24, 8)
	wColors["grass"] = p(7, 24, 8)
	wColors["sky"] = p(11, 22, 31)
	wColors["blue"] = p(6, 9, 30)
	wColors["purple"] = p(19, 7, 28)
	wColors["pink"] = p(31, 16, 24)
	wColors["brown"] = p(17, 10, 4)
	wColors["white"] = p(30, 30, 30)
	wColors["black"] = p(3, 3, 4)
	wColors["gray"] = p(18, 18, 19)
	wColors["gold"] = p(31, 25, 7)
	wColors["teal"] = p(6, 27, 26)
	wColors["lime"] = p(18, 31, 8)
}

func wColorOf(name string) uint16 {
	if c, ok := wColors[strings.ToLower(name)]; ok {
		return c
	}
	return wColors["pink"]
}

func wGrid(n int32) int32 { return n * 2 * wU }

func (w *World) addItem(mesh *engMesh, x, y, z int32, kind string) *wItem {
	it := &wItem{thing: engThing{mesh: mesh, x: x, y: y, z: z}, kind: kind, baseY: y}
	w.Items = append(w.Items, it)
	return it
}

func (w *World) Start(title string, quiet bool) {
	w.Title = title
	w.Items = nil
	w.HeroParts = nil
	w.GemsTotal = 0
	w.GemsGot = 0
	w.Msg = ""
	w.Frame = 0
	w.Won = false
	w.HasPlayer = false
	w.PX, w.PZ, w.DirF = 0, 0, 0
	if !quiet {
		fmt.Println("==============================")
		fmt.Println("  " + title)
		fmt.Println("  A tiny PS1 game. Have fun!")
		fmt.Println("==============================")
	}
}

func (w *World) MakeSky(name string) {
	c := wColorOf(name)
	w.Eng.setSky(engShade(c, 150), engShade(c, 256))
}

func (w *World) MakeGround(name string) {
	tex := int32(-1)
	key := strings.ToLower(name)
	switch key {
	case "grass", "green":
		tex = 0
	case "stone":
		tex = 1
	case "wood":
		tex = 3
	case "party":
		tex = 4
	case "sand", "yellow":
		tex = 5
	}
	w.addItem(engMeshGround(22*wU, tex, wColorOf(name)), 0, 0, 0, "ground")
}

func (w *World) AddTree(x, z int32) {
	browns := [6]uint16{engPack(17, 10, 4), engPack(17, 10, 4), engPack(15, 9, 4), engPack(19, 12, 5), engPack(17, 10, 4), engPack(12, 7, 3)}
	w.addItem(engMeshBox(wU/4, wU, wU/4, browns), wGrid(x), wU, wGrid(z), "tree")
	greens := [5]uint16{engPack(7, 24, 8), engPack(6, 21, 7), engPack(8, 26, 9), engPack(5, 19, 6), engPack(9, 20, 8)}
	w.addItem(engMeshPyramid(wU+wU/2, wU+wU/2, wU*2, greens), wGrid(x), wU*2, wGrid(z), "tree")
}

func (w *World) AddHouse(x, z int32, colName string) {
	c := wColorOf(colName)
	dark := engShade(c, 170)
	cols := [6]uint16{c, dark, dark, c, engPack(24, 9, 6), engPack(10, 8, 8)}
	w.addItem(engMeshBox(wU+wU/2, wU, wU+wU/2, cols), wGrid(x), wU, wGrid(z), "house")
	reds := [5]uint16{engPack(26, 8, 6), engPack(23, 7, 5), engPack(26, 8, 6), engPack(23, 7, 5), engPack(20, 6, 4)}
	w.addItem(engMeshPyramid(wU*2, wU*2, wU+wU/2, reds), wGrid(x), wU*2, wGrid(z), "house")
}

func (w *World) AddMountain(x, z int32, colName string) {
	c := wColorOf(colName)
	cols := [5]uint16{c, engShade(c, 200), c, engShade(c, 170), engShade(c, 120)}
	w.addItem(engMeshPyramid(wU*3, wU*3, wU*5, cols), wGrid(x), 0, wGrid(z), "mountain")
}

func (w *World) AddBox(x, z int32, colName string) {
	c := wColorOf(colName)
	cols := [6]uint16{c, engShade(c, 210), engShade(c, 180), c, engShade(c, 240), engShade(c, 130)}
	w.addItem(engMeshBox(wU*3/4, wU*3/4, wU*3/4, cols), wGrid(x), wU*3/4, wGrid(z), "box")
}

func (w *World) AddBall(x, z int32, colName string) {
	w.addItem(engMeshGem(wU*3/4, wU*3/4, wColorOf(colName)), wGrid(x), wU*3/4, wGrid(z), "ball")
}

func (w *World) AddGem(x, z int32) {
	w.addItem(engMeshGem(wU*3/4, wU, engPack(13, 30, 31)), wGrid(x), wU+wU/3, wGrid(z), "gem")
	w.GemsTotal++
}

func (w *World) PutPlayer(x, z int32, colName string) {
	c := wColorOf(colName)
	w.PX, w.PZ, w.DirF = wGrid(x), wGrid(z), 0
	w.HasPlayer = true
	front := engPack(30, 30, 30)
	cols := [6]uint16{front, engShade(c, 200), engShade(c, 180), c, engShade(c, 230), engShade(c, 120)}
	p1 := w.addItem(engMeshBox(wU/2+wU/8, wU/4, wU-wU/4, cols), w.PX, wU/4, w.PZ, "hero")
	w.HeroParts = append(w.HeroParts, p1)
	win := engPack(11, 22, 31)
	cols2 := [6]uint16{win, win, win, win, c, engShade(c, 120)}
	p2 := w.addItem(engMeshBox(wU/3, wU/5, wU/2, cols2), w.PX, wU/2+wU/5, w.PZ, "hero")
	w.HeroParts = append(w.HeroParts, p2)
}

func (w *World) Shout(text string) { w.Msg = text }

// ---------- rules ----------
func (w *World) blockedAt(nx, nz int32) bool {
	for _, it := range w.Items {
		switch it.kind {
		case "tree", "house", "mountain", "box", "ball":
			dx := int64(it.thing.x - nx)
			dz := int64(it.thing.z - nz)
			if dx*dx+dz*dz < 37748736 {
				return true
			}
		}
	}
	return false
}

func (w *World) checkGems() {
	for _, it := range w.Items {
		if it.kind == "gem" && !it.got {
			dx := int64(it.thing.x - w.PX)
			dz := int64(it.thing.z - w.PZ)
			if dx*dx+dz*dz < 42991616 {
				it.got = true
				w.GemsGot++
				w.Msg = "YOU GOT A GEM!"
				w.Eng.spawnSparkle(it.thing.x, it.thing.y, it.thing.z, engPack(31, 29, 6), 14)
				fmt.Println(w.GemsGot) // shfaq: reveal the new count
			}
		}
	}
	if w.GemsTotal > 0 && w.GemsGot >= w.GemsTotal {
		w.Won = true
	}
}

// tryMove attempts a fixed-point move; returns false on a bonk.
func (w *World) tryMove(dx, dz int32) bool {
	nx := w.PX + dx
	nz := w.PZ + dz
	if nx > 19*wU || nx < -19*wU || nz > 19*wU || nz < -19*wU {
		w.bonk("BONK! THE EDGE!")
		return false
	}
	if w.blockedAt(nx, nz) {
		w.bonk("BONK!")
		return false
	}
	w.PX, w.PZ = nx, nz
	return true
}

func (w *World) bonk(msg string) {
	if w.bonkCool <= 0 {
		w.Msg = msg
		w.bonkCool = 20
	}
}

type keyState struct{ Forward, Back, Left, Right bool }

// Update advances the live game by dt seconds.
func (w *World) Update(dt float64, k keyState) {
	if w.bonkCool > 0 {
		w.bonkCool--
	}
	if w.Won {
		return
	}
	turn := 100.0 // brads per second
	if k.Left {
		w.DirF -= turn * dt
	}
	if k.Right {
		w.DirF += turn * dt
	}
	dir := w.Dir()
	speed := float64(7 * wU) // fixed units per second
	var mf float64
	if k.Forward {
		mf = 1
	}
	if k.Back {
		mf = -1
	}
	if mf != 0 {
		// sinA/cosA are fixed (x4096): step = dir_unit * speed * dt
		dx := int32(float64(w.Eng.sinA(dir)) / 4096.0 * mf * speed * dt)
		dz := int32(float64(w.Eng.cosA(dir)) / 4096.0 * mf * speed * dt)
		w.tryMove(dx, dz)
	}
	w.checkGems()
}

// BotGreedy steers toward the nearest gem, one frame at dt=1/30.
func (w *World) BotGreedy() {
	if w.Won || w.GemsTotal == 0 {
		return
	}
	var best *wItem
	var bd int64 = -1
	for _, it := range w.Items {
		if it.kind == "gem" && !it.got {
			dx := int64(it.thing.x - w.PX)
			dz := int64(it.thing.z - w.PZ)
			d2 := dx*dx + dz*dz
			if bd < 0 || d2 < bd {
				bd = d2
				best = it
			}
		}
	}
	if best == nil {
		return
	}
	dx := float64(best.thing.x - w.PX)
	dz := float64(best.thing.z - w.PZ)
	want := math.Atan2(dx, dz) / (2 * math.Pi) * 256.0
	if want < 0 {
		want += 256
	}
	diff := want - w.DirF
	for diff > 128 {
		diff -= 256
	}
	for diff < -128 {
		diff += 256
	}
	turnStep := 100.0 / 30.0
	if diff > turnStep {
		w.DirF += turnStep
	} else if diff < -turnStep {
		w.DirF -= turnStep
	} else {
		w.DirF = want
	}
	if w.avoid > 0 {
		w.DirF += 3.0
		w.avoid--
	}
	dir := w.Dir()
	step := int32(7 * wU / 30)
	mx := w.Eng.sinA(dir) * step / 4096
	mz := w.Eng.cosA(dir) * step / 4096
	if !w.tryMove(mx, mz) {
		w.avoid = 25
	}
	w.checkGems()
}

// DiscreteStep is the Albish turn-based move, for play_bot.
func (w *World) DiscreteStep(ch rune) {
	switch ch {
	case 'w':
		dir := w.Dir()
		w.tryMove(w.Eng.sinA(dir), w.Eng.cosA(dir))
	case 's':
		dir := w.Dir()
		w.tryMove(-w.Eng.sinA(dir), -w.Eng.cosA(dir))
	case 'a':
		w.DirF -= 16
	case 'd':
		w.DirF += 16
	}
	w.checkGems()
}

// DrawFrame renders one frame of the world into the engine.
func (w *World) DrawFrame() {
	w.Frame++
	things := make([]engThing, 0, len(w.Items))
	for _, it := range w.Items {
		if it.kind == "gem" {
			it.thing.ry = int32((w.Frame * 2) % 256)
			it.thing.y = it.baseY + (w.Eng.sinA(int32((w.Frame*2)%256))*(wU/5))/wU
		}
		if !it.got {
			things = append(things, it.thing)
		}
	}
	for _, p := range w.HeroParts {
		p.thing.x = w.PX
		p.thing.z = w.PZ
		p.thing.ry = (256 - w.Dir()) % 256
	}
	dir := w.Dir()
	camX := w.PX - w.Eng.sinA(dir)*8
	camZ := w.PZ - w.Eng.cosA(dir)*8
	w.Eng.setCamera(camX, wU*4, camZ, dir, -14)
	w.Eng.renderScene(things)
	e := w.Eng
	e.drawText(8, 8, fmt.Sprintf("GEMS %d/%d", w.GemsGot, w.GemsTotal), engPack(31, 31, 31), 2)
	if w.Msg != "" {
		e.drawText(8, 218, w.Msg, engPack(31, 29, 6), 2)
	}
	if w.Won {
		e.drawText(76, 96, "YOU WIN", engPack(31, 29, 6), 4)
		e.drawText(88, 124, fmt.Sprintf("SCORE %d", w.GemsGot*100), engPack(31, 31, 31), 2)
	}
	e.stepParts()
}
