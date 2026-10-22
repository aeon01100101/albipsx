package main

// ============================================================
//  register.go - gives the Albish interpreter its "easy"
//  module, implemented natively. A game script that says
//  `import easy` gets these fast native words; a script run
//  under the plain Albish interpreter gets easy.alb instead.
//  Same words, same world - the script can't tell.
// ============================================================

type runControl struct {
	Headless      bool
	Window        bool
	Frames        int
	Shot          string
	Scale         int
	XCheck        bool
	Played        bool
	FramesRendered int
	BotMoves      []string
	HasBot        bool
}

func argInt(args []Value, i int) int32 {
	if i < len(args) {
		if v, ok := toIntValue(args[i]); ok {
			return int32(v)
		}
	}
	return 0
}

func argStr(args []Value, i int) string {
	if i < len(args) {
		return pyStr(args[i])
	}
	return ""
}

func registerEasy(in *Interp, w *World, ctl *runControl) {
	env := NewEnv(nil)
	def := func(name string, fn func(in *Interp, args []Value, kwargs map[string]Value) Value) {
		env.Define(name, &Builtin{Name: name, Fn: fn})
	}
	def("start", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.Start(argStr(args, 0), false)
		return nil
	})
	def("start_quiet", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.Start(argStr(args, 0), true)
		return nil
	})
	def("make_sky", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.MakeSky(argStr(args, 0))
		return nil
	})
	def("make_ground", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.MakeGround(argStr(args, 0))
		return nil
	})
	def("add_tree", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.AddTree(argInt(args, 0), argInt(args, 1))
		return nil
	})
	def("add_house", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.AddHouse(argInt(args, 0), argInt(args, 1), argStr(args, 2))
		return nil
	})
	def("add_mountain", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.AddMountain(argInt(args, 0), argInt(args, 1), argStr(args, 2))
		return nil
	})
	def("add_box", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.AddBox(argInt(args, 0), argInt(args, 1), argStr(args, 2))
		return nil
	})
	def("add_ball", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.AddBall(argInt(args, 0), argInt(args, 1), argStr(args, 2))
		return nil
	})
	def("add_gem", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.AddGem(argInt(args, 0), argInt(args, 1))
		return nil
	})
	def("put_player", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.PutPlayer(argInt(args, 0), argInt(args, 1), argStr(args, 2))
		return nil
	})
	def("shout", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		w.Shout(argStr(args, 0))
		return nil
	})
	def("play", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		ctl.Played = true
		runSession(w, ctl)
		return int64(w.GemsGot)
	})
	def("play_bot", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		moves := []string{}
		if len(args) > 0 {
			if lst, ok := args[0].(*List); ok {
				for _, el := range lst.Elems {
					moves = append(moves, pyStr(el))
				}
			}
		}
		ctl.Played = true
		ctl.BotMoves = moves
		ctl.HasBot = true
		runBotSession(w, ctl, moves)
		return int64(w.GemsGot)
	})
	in.Modules["easy"] = &Module{Name: "easy", Env: env}
}
