package main

import "testing"

// The floor must never tear open: wherever the hero stands,
// the bottom rows of the picture are ground, never sky.
// (Whole-triangle near rejection used to tear a hole in the
// tile the camera stood in; near-plane clipping fixed it.)
func TestGroundNeverTears(t *testing.T) {
	for _, pz := range []int32{-49152, -32768, -16384, 0, 16384, 32768, 49152} {
		for _, px := range []int32{-16384, 0, 16384} {
			w := newWorld()
			w.Start("T", true)
			w.MakeSky("sky")
			w.MakeGround("grass")
			w.AddGem(0, -2)
			w.PutPlayer(0, -6, "red")
			w.PX, w.PZ = px, pz
			w.DrawFrame()
			sky := w.Eng.SkyBot
			bad := 0
			for y := 225; y < 240; y++ {
				for x := 0; x < 320; x++ {
					if w.Eng.FB[y*scrW+x] == sky {
						bad++
					}
				}
			}
			if bad > 0 {
				t.Errorf("px %d pz %d: %d sky pixels in bottom rows", px, pz, bad)
			}
		}
	}
}
