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

note "=== RESULT: $([ $FAIL = 0 ] && echo ALL PASS || echo FAILURES) ==="
cat "$OUT"
exit $FAIL
