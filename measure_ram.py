#!/usr/bin/env python3
"""Run a command and report its peak memory (RSS) and time.

The rule for AlbiPSX: a whole game must stay under 250 MB.
Peak RSS of the child process is the honest measure."""
import resource
import subprocess
import sys
import time

LIMIT_MB = 250.0


def main() -> int:
    if not sys.argv[1:]:
        print("usage: measure_ram.py <command...>")
        return 2
    start = time.time()
    proc = subprocess.run(sys.argv[1:])
    elapsed = time.time() - start
    peak_kb = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss
    peak_mb = peak_kb / 1024.0
    status = "UNDER" if peak_mb < LIMIT_MB else "OVER"
    print(
        f"RAM: peak {peak_mb:.1f} MB (limit {LIMIT_MB:.0f} MB) -> {status}"
        f" | time {elapsed:.2f}s | exit {proc.returncode}"
    )
    if proc.returncode != 0:
        return proc.returncode
    return 0 if peak_mb < LIMIT_MB else 1


if __name__ == "__main__":
    raise SystemExit(main())
