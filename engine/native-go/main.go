package main

// ============================================================
//  AlbiPSX native - the actual Linux program.
//
//    albipsx game.alb                 window play (X11)
//    albipsx game.alb --headless      the robot plays
//    albipsx game.alb --shot out.png  save a picture
//    albipsx --selftest               prove the z-buffer
//
//  The game file is plain Albish and uses the friendly
//  easy.* words - which are native inside this program.
//  One static binary: the Albish interpreter, the PS1
//  renderer WITH z-buffer, and the window code, all in.
// ============================================================

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const nativeVersion = "1.1.0"

// VERSION is the embedded Albish language version (it lived
// in the interpreter's own main.go, which this program
// replaces with its own).
const VERSION = "2.0.0"

func main() {
	os.Exit(runApp(os.Args[1:]))
}

// reorderArgs lets flags come before OR after the game
// file ("albipsx game.alb --headless"), which is how people
// naturally type it. Go's flag package alone stops at the
// first non-flag word and would silently drop the rest.
func reorderArgs(argv []string) []string {
	valueFlags := map[string]bool{"frames": true, "shot": true, "scale": true}
	var flags, pos []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		name := ""
		if strings.HasPrefix(a, "--") {
			name = a[2:]
		} else if strings.HasPrefix(a, "-") && len(a) > 1 {
			name = a[1:]
		}
		if name == "" {
			pos = append(pos, a)
			continue
		}
		if eq := strings.Index(name, "="); eq >= 0 {
			name = name[:eq]
			flags = append(flags, a)
			continue
		}
		flags = append(flags, a)
		if valueFlags[name] && i+1 < len(argv) {
			i++
			flags = append(flags, argv[i])
		}
	}
	return append(flags, pos...)
}

func runApp(argv []string) (code int) {
	argv = reorderArgs(argv)
	fs := flag.NewFlagSet("albipsx", flag.ContinueOnError)
	headless := fs.Bool("headless", false, "no window: the robot plays")
	window := fs.Bool("window", false, "force a window (needs DISPLAY)")
	frames := fs.Int("frames", 0, "stop after N frames (0 = until quit or win)")
	shot := fs.String("shot", "", "save the last frame as this .png (or .ppm)")
	scale := fs.Int("scale", 3, "window size multiplier (1-4)")
	xcheck := fs.Bool("xcheck", false, "read the window back and check its pixels")
	selftest := fs.Bool("selftest", false, "run renderer + z-buffer checks and exit")
	version := fs.Bool("version", false, "print the version")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *version {
		fmt.Println("AlbiPSX native " + nativeVersion + " (Albish " + VERSION + " inside)")
		return 0
	}
	if *selftest {
		return runSelfTest()
	}
	game := "game.alb"
	if fs.NArg() > 0 {
		game = fs.Arg(0)
	}
	data, err := os.ReadFile(game)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read game file:", err)
		return 1
	}
	ctl := &runControl{
		Headless: *headless,
		Window:   *window,
		Frames:   *frames,
		Shot:     *shot,
		Scale:    *scale,
		XCheck:   *xcheck,
	}
	if *window {
		ctl.Headless = false
	} else if !*headless && os.Getenv("DISPLAY") == "" {
		ctl.Headless = true
		fmt.Println("No screen found (DISPLAY is empty) - running headless.")
	}
	w := newWorld()
	in := NewInterp(game)
	registerEasy(in, w, ctl)
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *AlbError:
				fmt.Fprintln(os.Stderr, e.Error())
				code = 1
			case returnSignal, breakSignal, continueSignal:
				// top-level flow signals: the original ignores them too
			default:
				fmt.Fprintf(os.Stderr, "InternalError: %v\n", r)
				code = 1
			}
		}
	}()
	prog := NewParser(tokenize(string(data)), game).Parse()
	in.Run(prog)
	if !ctl.Played && ctl.Shot != "" && len(w.Items) > 0 {
		w.DrawFrame()
		ctl.FramesRendered++
		saveShot(w, ctl)
	}
	return code
}
