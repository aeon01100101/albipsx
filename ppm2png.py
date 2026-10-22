#!/usr/bin/env python3
"""Turn an AlbiPSX PPM picture (P3, max 31) into a PNG.
Plain Python standard library only - no installs needed."""
import struct
import sys
import zlib


def main() -> int:
    src, dst = sys.argv[1], sys.argv[2]
    with open(src) as f:
        magic = f.readline().strip()
        if magic != "P3":
            print(f"not a P3 ppm: {magic!r}")
            return 1
        w, h = map(int, f.readline().split())
        maxv = int(f.readline())
        data = []
        for line in f:
            data.extend(map(int, line.split()))
    if len(data) != w * h * 3:
        print(f"bad pixel count: {len(data)} != {w*h*3}")
        return 1
    scale = 255 // maxv
    raw = bytearray()
    i = 0
    for _y in range(h):
        raw.append(0)
        for _x in range(w):
            raw += bytes((data[i] * scale, data[i + 1] * scale, data[i + 2] * scale))
            i += 3

    def chunk(kind: bytes, payload: bytes) -> bytes:
        return (
            struct.pack(">I", len(payload))
            + kind
            + payload
            + struct.pack(">I", zlib.crc32(kind + payload) & 0xFFFFFFFF)
        )

    png = b"\x89PNG\r\n\x1a\n"
    png += chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress(bytes(raw)))
    png += chunk(b"IEND", b"")
    with open(dst, "wb") as f:
        f.write(png)
    print(f"png ok: {dst} ({w}x{h})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
