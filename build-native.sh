#!/bin/bash
# ============================================================
#  build-native.sh - builds THE ENGINE:
#    engine/albipsx   (one static C binary that fits a PS1)
#  and tools/measure (the tiny honest RAM measurer).
#  The engine lives in engine/. Games live in games/.
#  No libraries, no runtime: everything is static arrays.
# ============================================================
set -e
cd "$(dirname "$0")"
CC=${CC:-gcc}
$CC -Os -s -static -ffunction-sections -fdata-sections -Wl,--gc-sections \
    -Wall -Wextra -Wno-unused-parameter \
    -o engine/albipsx engine/native/render.c engine/native/world.c engine/native/x11.c engine/native/main.c
$CC -O2 -static -o tools/measure tools/measure.c
echo "built: engine/albipsx ($(stat -c%s engine/albipsx) bytes, static, PS1-sized)"
./engine/albipsx --version
./engine/albipsx --selftest | tail -n 1
