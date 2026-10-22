#!/bin/bash
# ============================================================
#  build-native.sh - builds the actual Linux program:
#    ./albipsx   (one static C binary that fits a PS1)
#  and ./measure (the tiny honest RAM measurer).
#  No libraries, no runtime: everything is static arrays.
# ============================================================
set -e
cd "$(dirname "$0")"
CC=${CC:-gcc}
$CC -Os -s -static -ffunction-sections -fdata-sections -Wl,--gc-sections \
    -Wall -Wextra -Wno-unused-parameter \
    -o albipsx native/render.c native/world.c native/x11.c native/main.c
$CC -O2 -static -o measure measure.c
echo "built: ./albipsx ($(stat -c%s ./albipsx) bytes, static, PS1-sized)"
./albipsx --version
./albipsx --selftest | tail -n 1
