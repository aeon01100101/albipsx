package main

// ============================================================
//  session.go - actually playing the game: a real X11
//  window when there's a desktop, a headless bot when
//  there isn't (or when --headless says so).
// ============================================================

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func saveShot(w *World, ctl *runControl) {
	if ctl.Shot == "" {
		return
	}
	if strings.HasSuffix(ctl.Shot, ".ppm") {
		f, err := os.Create(ctl.Shot)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shot:", err)
			return
		}
		defer f.Close()
		fmt.Fprintln(f, "P3")
		fmt.Fprintln(f, "320 240")
		fmt.Fprintln(f, "31")
		e := w.Eng
		for y := 0; y < scrH; y++ {
			var sb strings.Builder
			for x := 0; x < scrW; x++ {
				c := e.FB[y*scrW+x]
				fmt.Fprintf(&sb, "%d %d %d ", engRed(c), engGrn(c), engBlu(c))
			}
			fmt.Fprintln(f, sb.String())
		}
		fmt.Println("saved", ctl.Shot)
		return
	}
	if err := w.Eng.savePNG(ctl.Shot); err != nil {
		fmt.Fprintln(os.Stderr, "shot:", err)
		return
	}
	fmt.Println("saved", ctl.Shot)
}

func summary(w *World, ctl *runControl, how string) {
	fmt.Printf("SESSION frames %d gems %d of %d won %v (%s)\n",
		ctl.FramesRendered, w.GemsGot, w.GemsTotal, w.Won, how)
}

// runSession is easy.play(): live window play, or the
// greedy robot when headless.
func runSession(w *World, ctl *runControl) {
	if ctl.Headless {
		runHeadlessBot(w, ctl)
		return
	}
	if err := runWindow(w, ctl); err != nil {
		fmt.Fprintln(os.Stderr, "window:", err)
		fmt.Fprintln(os.Stderr, "falling back to the headless robot")
		runHeadlessBot(w, ctl)
	}
}

func runHeadlessBot(w *World, ctl *runControl) {
	frames := ctl.Frames
	if frames <= 0 {
		frames = 1800
	}
	extra := 0
	for f := 0; f < frames; f++ {
		w.BotGreedy()
		w.DrawFrame()
		ctl.FramesRendered++
		if w.Won {
			extra++
			if extra > 20 {
				break
			}
		}
	}
	saveShot(w, ctl)
	summary(w, ctl, "headless robot")
}

// runBotSession is easy.play_bot(moves): the typed-move
// robot, one rendered frame per line of moves.
func runBotSession(w *World, ctl *runControl, moves []string) {
	var present func()
	var x *xConn
	var win, gc uint32
	if !ctl.Headless {
		var err error
		x, err = xDial()
		if err == nil {
			winW, winH := scrW*ctl.Scale, scrH*ctl.Scale
			win, gc, err = x.createWindow(winW, winH, "AlbiPSX - "+w.Title)
			if err == nil {
				present = func() { x.putImageScaled(win, gc, w.Eng.FB, ctl.Scale) }
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "window:", err, "- running headless")
		}
	}
	w.DrawFrame()
	ctl.FramesRendered++
	if present != nil {
		present()
		time.Sleep(200 * time.Millisecond)
	}
	for _, line := range moves {
		fmt.Println("The robot player types: " + line)
		for _, ch := range line {
			w.DiscreteStep(ch)
		}
		w.DrawFrame()
		ctl.FramesRendered++
		if present != nil {
			present()
			time.Sleep(350 * time.Millisecond)
		}
	}
	if w.Won {
		fmt.Println("********************************")
		fmt.Println("  YOU WIN! YOU FOUND ALL THE GEMS!")
		fmt.Printf("  Score: %d\n", w.GemsGot*100)
		fmt.Println("********************************")
	}
	fmt.Printf("BOT DONE GEMS %d OF %d\n", w.GemsGot, w.GemsTotal)
	saveShot(w, ctl)
	_ = x
	_ = gc
}

// runWindow is the real Linux program: an X11 window, real
// keys, 30 frames a second, until q / Esc / window close.
func runWindow(w *World, ctl *runControl) error {
	x, err := xDial()
	if err != nil {
		return err
	}
	scale := ctl.Scale
	if scale < 1 {
		scale = 1
	}
	if scale > 4 {
		scale = 4
	}
	winW, winH := scrW*scale, scrH*scale
	win, gc, err := x.createWindow(winW, winH, "AlbiPSX - "+w.Title)
	if err != nil {
		return err
	}
	protocols, delAtom := x.setWMDelete(win)
	_ = protocols
	km := x.keyboardMapping()
	codeFor := func(sym uint32) byte { return km[sym] }
	keys := map[byte]string{}
	bind := func(sym uint32, action string) {
		if c, ok := km[sym]; ok {
			keys[c] = action
		}
	}
	bind(0x77, "forward") // w
	bind(0x61, "left")    // a
	bind(0x73, "back")    // s
	bind(0x64, "right")   // d
	bind(0x71, "quit")    // q
	bind(0xff1b, "quit")  // Escape
	bind(0xff52, "forward")
	bind(0xff51, "left")
	bind(0xff54, "back")
	bind(0xff53, "right")
	_ = codeFor

	state := map[string]bool{}
	quit := false
	fmt.Println("Window open: w a s d or arrow keys to drive, q to stop.")
	last := time.Now()
	frames := 0
	xchecked := false
	for !quit {
		// drain events
		draining := true
		for draining {
			select {
			case ev := <-x.events:
				switch ev[0] & 0x7f {
				case 2: // KeyPress
					if a, ok := keys[ev[1]]; ok {
						if a == "quit" {
							quit = true
						} else {
							state[a] = true
						}
					}
				case 3: // KeyRelease
					if a, ok := keys[ev[1]]; ok {
						state[a] = false
					}
				case 33: // ClientMessage
					if delAtom != 0 && len(ev) >= 16 {
						v := uint32(ev[12]) | uint32(ev[13])<<8 | uint32(ev[14])<<16 | uint32(ev[15])<<24
						if v == delAtom {
							quit = true
						}
					}
				}
			default:
				draining = false
			}
		}
		now := time.Now()
		dt := now.Sub(last).Seconds()
		last = now
		if dt > 0.05 {
			dt = 0.05
		}
		if dt <= 0 {
			dt = 0.001
		}
		w.Update(dt, keyState{
			Forward: state["forward"], Back: state["back"],
			Left: state["left"], Right: state["right"],
		})
		w.DrawFrame()
		ctl.FramesRendered++
		frames++
		x.putImageScaled(win, gc, w.Eng.FB, scale)
		if ctl.XCheck && !xchecked && frames >= 5 {
			xchecked = true
			xCheckWindow(x, win, winW, winH, w)
		}
		if ctl.Frames > 0 && frames >= ctl.Frames {
			break
		}
		// rest until ~30 fps
		elapsed := time.Since(now)
		if elapsed < 33*time.Millisecond {
			time.Sleep(33*time.Millisecond - elapsed)
		}
	}
	if x.lastErr != "" {
		fmt.Fprintln(os.Stderr, "X reported:", x.lastErr)
	}
	saveShot(w, ctl)
	summary(w, ctl, "window")
	return nil
}

// xCheckWindow reads the window back and proves the pixels
// the player sees match the framebuffer we painted.
func xCheckWindow(x *xConn, win uint32, winW, winH int, w *World) {
	data := x.getImage(win, winW, winH)
	if data == nil || len(data) < winW*winH*4 {
		fmt.Println("XCHECK FAIL no image came back")
		return
	}
	vis, ok := x.visuals[x.rootVisual]
	if !ok {
		vis = xVisual{redMask: 0xff0000, greenMask: 0xff00, blueMask: 0xff}
	}
	rsh := shiftFor(vis.redMask)
	gsh := shiftFor(vis.greenMask)
	bsh := shiftFor(vis.blueMask)
	// sample a grid of points: window pixel vs framebuffer pixel
	bad := 0
	checked := 0
	for gy := 0; gy < 12; gy++ {
		for gx := 0; gx < 16; gx++ {
			wx := gx*(winW-8)/16 + 4
			wy := gy*(winH-8)/12 + 4
			off := (wy*winW + wx) * 4
			var v uint32
			if x.imgLSB {
				v = uint32(data[off]) | uint32(data[off+1])<<8 | uint32(data[off+2])<<16 | uint32(data[off+3])<<24
			} else {
				v = uint32(data[off])<<24 | uint32(data[off+1])<<16 | uint32(data[off+2])<<8 | uint32(data[off+3])
			}
			r8 := (v >> rsh) & 0xff
			g8 := (v >> gsh) & 0xff
			b8 := (v >> bsh) & 0xff
			fx := wx * scrW / winW
			fy := wy * scrH / winH
			c := w.Eng.FB[fy*scrW+fx]
			er := int64(engRed(c) * 255 / 31)
			eg := int64(engGrn(c) * 255 / 31)
			eb := int64(engBlu(c) * 255 / 31)
			diff := abs64(int64(r8)-er) + abs64(int64(g8)-eg) + abs64(int64(b8)-eb)
			checked++
			if diff > 24 {
				bad++
			}
		}
	}
	if bad == 0 {
		fmt.Printf("XCHECK PASS window pixels match the framebuffer (%d points)\n", checked)
	} else {
		fmt.Printf("XCHECK FAIL %d of %d points differ\n", bad, checked)
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
