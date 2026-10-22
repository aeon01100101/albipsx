#!/usr/bin/env python3
"""Run a command and report its TRUE peak memory (RSS) and time.

Honest measurement needs a tiny launcher: fork() inherits the
parent's rusage high-water mark, so measuring from Python (or
any big parent) makes every child look huge. This script runs
the command through ./measure - a few-pages-big C launcher
(measure.c) whose wait4() reports the child's own peak.

Limit: 250 MB by default (the original AlbiPSX rule). Set
ALBIPSX_LIMIT_MB=2 for the PS1 rule (2 MB main RAM)."""
import os
import re
import subprocess
import sys

LIMIT_MB = float(os.environ.get("ALBIPSX_LIMIT_MB", "250"))
HERE = os.path.dirname(os.path.abspath(__file__))
MEASURE = os.path.join(HERE, "measure")


def ensure_measure() -> bool:
    if os.access(MEASURE, os.X_OK):
        return True
    src = os.path.join(HERE, "measure.c")
    if os.path.exists(src):
        rc = subprocess.run(
            ["gcc", "-O2", "-static", "-o", MEASURE, src],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        ).returncode
        return rc == 0 and os.access(MEASURE, os.X_OK)
    return False


def main() -> int:
    if not sys.argv[1:]:
        print("usage: measure_ram.py <command...>")
        return 2
    if not ensure_measure():
        print("measure_ram.py: ./measure helper missing and gcc unavailable",
              file=sys.stderr)
        return 2
    proc = subprocess.run([MEASURE] + sys.argv[1:],
                          stdout=subprocess.PIPE, text=True)
    sys.stdout.write(proc.stdout)
    m = re.search(r"RAM: peak ([0-9.]+) MB \| time ([0-9.]+)s \| exit (\d+)",
                  proc.stdout)
    if not m:
        return proc.returncode or 1
    peak_mb, elapsed, code = float(m.group(1)), float(m.group(2)), int(m.group(3))
    status = "UNDER" if peak_mb < LIMIT_MB else "OVER"
    print(f"RAM: peak {peak_mb:.1f} MB (limit {LIMIT_MB:.0f} MB) -> {status}"
          f" | time {elapsed:.2f}s | exit {code}")
    if code != 0:
        return code
    return 0 if peak_mb < LIMIT_MB else 1


if __name__ == "__main__":
    raise SystemExit(main())
