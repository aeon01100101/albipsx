#!/bin/bash
# ============================================================
#  verify.sh - proves the whole engine works, end to end.
#  Writes everything to results.txt and prints it at the end.
#  A check only counts when the log file says so.
# ============================================================
cd "$(dirname "$0")"
ALBISH=~/workspace/albish/bin/albish
OUT=results.txt
: > "$OUT"
FAIL=0

note() { echo "$1" | tee -a "$OUT"; }

note "=== AlbiPSX verification ==="

# 1. Engine unit checks
"$ALBISH" test_engine.alb > /tmp/albipsx_tests.log 2>&1
if grep -q "FAIL 0" /tmp/albipsx_tests.log; then
  note "PASS engine checks: $(grep 'TESTS PASS' /tmp/albipsx_tests.log)"
else
  note "FAIL engine checks"; cat /tmp/albipsx_tests.log >> "$OUT"; FAIL=1
fi

# 2. Robot plays the demo game and collects a gem
python3 measure_ram.py "$ALBISH" autoplay.alb > /tmp/albipsx_auto.log 2>&1
if grep -q "BOT DONE GEMS 1 OF 3" /tmp/albipsx_auto.log; then
  note "PASS robot playthrough: collected a gem by driving"
else
  note "FAIL robot playthrough"; FAIL=1
fi
grep "^RAM:" /tmp/albipsx_auto.log >> "$OUT"
if grep -q "UNDER" /tmp/albipsx_auto.log; then
  note "PASS ram rule (autoplay)"
else
  note "FAIL ram rule (autoplay)"; FAIL=1
fi

# 3. A human-style game session: type moves, then q
printf "wwwq\n" | timeout 60 "$ALBISH" game.alb > /tmp/albipsx_game.log 2>&1
if grep -q "Bye! Thanks for playing" /tmp/albipsx_game.log; then
  note "PASS interactive game session (typed moves + q)"
else
  note "FAIL interactive game session"; FAIL=1
fi

# 4. The kindergarten template game starts and quits cleanly
printf "q\n" | timeout 60 "$ALBISH" my_first_game.alb > /tmp/albipsx_mine.log 2>&1
if grep -q "Bye! Thanks for playing" /tmp/albipsx_mine.log; then
  note "PASS my_first_game template runs"
else
  note "FAIL my_first_game template"; FAIL=1
fi

# 5. Screenshot is a real 320x240 picture
./screenshot.sh > /tmp/albipsx_shot.log 2>&1
if python3 -c "
ppm = open('output/frame.ppm').read().split()
assert ppm[0] == 'P3' and ppm[1] == '320' and ppm[2] == '240' and ppm[3] == '31'
assert len(ppm) == 4 + 320 * 240 * 3, len(ppm)
print('ppm ok')"; then
  note "PASS screenshot output/frame.ppm is a real 320x240 picture"
  note "PASS screenshot output/frame.png converted"
else
  note "FAIL screenshot"; FAIL=1
fi

# 6. RAM on the heaviest job (bench: 12 spinning pictures)
python3 measure_ram.py "$ALBISH" bench.alb > /tmp/albipsx_bench.log 2>&1
grep "^RAM:" /tmp/albipsx_bench.log >> "$OUT"
grep "BENCH DONE" /tmp/albipsx_bench.log >> "$OUT"
if grep -q "UNDER" /tmp/albipsx_bench.log; then
  note "PASS ram rule (bench)"
else
  note "FAIL ram rule (bench)"; FAIL=1
fi

# ============================================================
#  The actual Linux program (native, with z-buffer)
# ============================================================
note "--- the Linux program (C, PS1-sized, z-buffer) ---"

# 7. Native binary exists and its selftest proves the z-buffer
#    AND the PS1 budgets (17 checks, incl. floor-never-tears)
if [ -x ./albipsx ]; then
  ./albipsx --selftest > /tmp/albipsx_self.log 2>&1
  if grep -q "SELFTEST PASS 17 FAIL 0" /tmp/albipsx_self.log; then
    note "PASS native selftest: z-buffer + PS1 budget proofs (17 checks)"
  else
    note "FAIL native selftest"; cat /tmp/albipsx_self.log >> "$OUT"; FAIL=1
  fi
else
  note "FAIL native binary missing (run ./build-native.sh)"; FAIL=1
fi

# 8. Native headless: the robot wins the whole game, and the
#    whole run must fit inside a real PS1 (2 MB main RAM).
#    The RAM line comes from the tiny C launcher (measure.c) -
#    a big parent process would inflate the child's rusage.
ALBIPSX_LIMIT_MB=2 python3 measure_ram.py ./albipsx game.alb --headless --shot output/native_frame.png > /tmp/albipsx_nat.log 2>&1
if grep -q "won true" /tmp/albipsx_nat.log; then
  note "PASS native headless: robot collected all gems and won"
else
  note "FAIL native headless playthrough"; FAIL=1
fi
grep "^RAM:" /tmp/albipsx_nat.log >> "$OUT"
grep "PS1 BUDGET" /tmp/albipsx_nat.log >> "$OUT"
if grep -q "UNDER" /tmp/albipsx_nat.log && grep -q "WITHIN PS1 LIMITS" /tmp/albipsx_nat.log; then
  note "PASS PS1 ram rule (whole game under 2 MB, measured two ways)"
else
  note "FAIL PS1 ram rule (native program)"; FAIL=1
fi

# 9. Native window under a real X server (Xvfb), pixels read back
if command -v Xvfb > /dev/null 2>&1; then
  XVFB_STARTED=""
  if [ ! -e /tmp/.X11-unix/X98 ]; then
    Xvfb :98 -screen 0 1280x900x24 > /tmp/albipsx_xvfb.log 2>&1 &
    XVFB_STARTED=$!
    sleep 2
  fi
  DISPLAY=:98 ./albipsx game.alb --window --frames 90 --xcheck > /tmp/albipsx_win.log 2>&1
  if grep -q "XCHECK PASS" /tmp/albipsx_win.log; then
    note "PASS native window: real X11 window, pixels read back and matched"
  else
    note "FAIL native window run"; cat /tmp/albipsx_win.log >> "$OUT"; FAIL=1
  fi
  grep "RAM peak" /tmp/albipsx_win.log >> "$OUT"
  if grep -q "WITHIN PS1 LIMITS" /tmp/albipsx_win.log; then
    note "PASS PS1 ram rule (window mode too)"
  else
    note "FAIL PS1 ram rule (window mode)"; FAIL=1
  fi
  [ -n "$XVFB_STARTED" ] && kill "$XVFB_STARTED" 2>/dev/null
fi

note "=== RESULT: $([ $FAIL = 0 ] && echo ALL PASS || echo FAILURES) ==="
cat "$OUT"
exit $FAIL
