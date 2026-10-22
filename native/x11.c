/* ============================================================
   x11.c - the tiny X11 client. Pure sockets + syscalls.
   Single-threaded: replies are waited for synchronously and
   any events that arrive meanwhile are stashed for later.
   ============================================================ */
#include "x11.h"
#include "psx.h"
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <sys/socket.h>
#include <sys/un.h>

static uint8_t rbuf[16384];
static int rpos = 0, rlen = 0;      /* buffered bytes are rbuf[rpos..rlen) */
static int g_img_lsb = 1;
static uint32_t g_rid_base = 0, g_rid_mask = 0, g_rid_next = 0;
static uint8_t band_buf[32768];

static void buf_compact(void) {
    if (rpos > 0) {
        memmove(rbuf, rbuf + rpos, (size_t)(rlen - rpos));
        rlen -= rpos;
        rpos = 0;
    }
}

/* ensure at least n bytes buffered; block=1 waits. returns buffered count */
static int buf_fill(XConn *x, int n, int block) {
    while (rlen - rpos < n) {
        buf_compact();
        if (rlen >= (int)sizeof rbuf) return rlen - rpos;
        int flags = block ? 0 : MSG_DONTWAIT;
        ssize_t m = recv(x->fd, rbuf + rlen, sizeof rbuf - (size_t)rlen, flags);
        if (m > 0) { rlen += (int)m; continue; }
        if (m == 0) return rlen - rpos;               /* closed */
        if (!block && (errno == EAGAIN || errno == EWOULDBLOCK)) return rlen - rpos;
        if (errno == EINTR) continue;
        return rlen - rpos;
    }
    return rlen - rpos;
}

static int x_send_raw(XConn *x, const uint8_t *buf, int len) {
    int off = 0;
    while (off < len) {
        ssize_t m = send(x->fd, buf + off, (size_t)(len - off), 0);
        if (m < 0) {
            if (errno == EINTR) continue;
            snprintf(x->err, sizeof x->err, "send failed");
            return -1;
        }
        off += (int)m;
    }
    return 0;
}

/* one call per X REQUEST: the sequence number counts requests,
   not send() calls (a big request may need several sends) */
static int x_send(XConn *x, const uint8_t *buf, int len) {
    x->seq++;
    return x_send_raw(x, buf, len);
}

/* read one packet. Returns: 1 event (32B in out), 2 reply (full in out,
   *outlen set, may exceed 32), 0 error packet, -1 nothing yet (nonblock),
   -2 connection trouble. */
static int next_packet(XConn *x, uint8_t *out, int outcap, int *outlen, int block) {
    if (x->evq_head != x->evq_tail && out != NULL && !block) {
        /* events first when polling */
    }
    if (buf_fill(x, 32, block) < 32) return -1;
    uint8_t b0 = rbuf[rpos];
    int total = 32;
    if (b0 == 1) {
        uint32_t extra = (uint32_t)rbuf[rpos + 4] | ((uint32_t)rbuf[rpos + 5] << 8) |
                         ((uint32_t)rbuf[rpos + 6] << 16) | ((uint32_t)rbuf[rpos + 7] << 24);
        total = 32 + (int)extra * 4;
        if (total > outcap) {
            /* reply too big for the buffer: consume and report truncated */
            int left = total;
            while (left > 0) {
                int avail = buf_fill(x, 1, 1);
                if (avail <= 0) return -2;
                int take = avail < left ? avail : left;
                rpos += take;
                left -= take;
                buf_compact();
            }
            *outlen = total;
            return 3;
        }
    }
    if (buf_fill(x, total, 1) < total) return -2;
    memcpy(out, rbuf + rpos, (size_t)total);
    rpos += total;
    buf_compact();
    *outlen = total;
    if (b0 == 0) return 0;
    if (b0 == 1) return 2;
    return 1;
}

static void stash_event(XConn *x, const uint8_t *ev) {
    int next = (x->evq_tail + 1) % 64;
    if (next == x->evq_head) return; /* full: drop */
    memcpy(x->evq[x->evq_tail], ev, 32);
    x->evq_tail = next;
}

/* wait for the reply to request seq_want, stashing events */
static int wait_reply(XConn *x, uint16_t seq_want, uint8_t *out, int outcap, int *outlen) {
    static uint8_t pkt[16384];
    for (;;) {
        int plen = 0;
        int kind = next_packet(x, pkt, (int)sizeof pkt, &plen, 1);
        if (kind == 1) { stash_event(x, pkt); continue; }
        if (kind == 0) {
            snprintf(x->err, sizeof x->err, "X error code %d on request %d",
                     pkt[1], seq_want);
            return -1;
        }
        if (kind == 2) {
            uint16_t seq = (uint16_t)(pkt[2] | (pkt[3] << 8));
            if (seq == seq_want) {
                if (plen > outcap) return -1;
                memcpy(out, pkt, (size_t)plen);
                *outlen = plen;
                return 0;
            }
            continue; /* a reply we didn't wait for: drop */
        }
        return -1;
    }
}

int x_poll_event(XConn *x, uint8_t ev[32]) {
    if (x->evq_head != x->evq_tail) {
        memcpy(ev, x->evq[x->evq_head], 32);
        x->evq_head = (x->evq_head + 1) % 64;
        return 1;
    }
    int plen = 0;
    static uint8_t pkt[16384];
    int kind = next_packet(x, pkt, (int)sizeof pkt, &plen, 0);
    if (kind == 1) {
        memcpy(ev, pkt, 32);
        return 1;
    }
    if (kind == 0) {
        snprintf(x->err, sizeof x->err, "X error code %d", pkt[1]);
    }
    return 0;
}

/* ---------------- setup ---------------- */
static uint16_t rd16(const uint8_t *p) { return (uint16_t)(p[0] | (p[1] << 8)); }
static uint32_t rd32(const uint8_t *p) {
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}
static void wr16(uint8_t *p, uint16_t v) { p[0] = (uint8_t)v; p[1] = (uint8_t)(v >> 8); }
static void wr32(uint8_t *p, uint32_t v) {
    p[0] = (uint8_t)v; p[1] = (uint8_t)(v >> 8); p[2] = (uint8_t)(v >> 16); p[3] = (uint8_t)(v >> 24);
}

int x_open(XConn *x) {
    memset(x, 0, sizeof *x);
    x->fd = -1;
    const char *disp = getenv("DISPLAY");
    if (!disp || !disp[0]) {
        snprintf(x->err, sizeof x->err, "DISPLAY is not set");
        return -1;
    }
    const char *colon = strrchr(disp, ':');
    if (!colon) {
        snprintf(x->err, sizeof x->err, "odd DISPLAY");
        return -1;
    }
    char num[16];
    snprintf(num, sizeof num, "%s", colon + 1);
    char *dot = strchr(num, '.');
    if (dot) *dot = 0;
    struct sockaddr_un addr;
    memset(&addr, 0, sizeof addr);
    addr.sun_family = AF_UNIX;
    snprintf(addr.sun_path, sizeof addr.sun_path, "/tmp/.X11-unix/X%s", num);
    int fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd < 0) {
        snprintf(x->err, sizeof x->err, "socket failed");
        return -1;
    }
    if (connect(fd, (struct sockaddr *)&addr, sizeof addr) < 0) {
        snprintf(x->err, sizeof x->err, "cannot reach the X server at %s", disp);
        close(fd);
        return -1;
    }
    x->fd = fd;
    rpos = rlen = 0;
    uint8_t setup[12];
    memset(setup, 0, sizeof setup);
    setup[0] = 'l';
    wr16(setup + 2, 11);
    if (x_send_raw(x, setup, 12) < 0) return -1;
    x->seq = 0; /* setup is not a numbered request */
    uint8_t head[8];
    if (buf_fill(x, 8, 1) < 8) {
        snprintf(x->err, sizeof x->err, "no setup reply");
        return -1;
    }
    memcpy(head, rbuf + rpos, 8);
    rpos += 8;
    if (head[0] == 0) {
        snprintf(x->err, sizeof x->err, "X server refused the connection");
        return -1;
    }
    if (head[0] != 1) {
        snprintf(x->err, sizeof x->err, "X setup: odd status %d", head[0]);
        return -1;
    }
    int extra = rd16(head + 6) * 4;
    static uint8_t body[8192];
    if (extra > (int)sizeof body) extra = (int)sizeof body;
    if (buf_fill(x, extra, 1) < extra) return -1;
    memcpy(body, rbuf + rpos, (size_t)extra);
    rpos += extra;
    buf_compact();
    g_rid_base = rd32(body + 4);
    g_rid_mask = rd32(body + 8);
    g_rid_next = 0;
    int vendor_len = rd16(body + 16);
    x->max_req_len = rd16(body + 18);
    int num_roots = body[20], num_fmt = body[21];
    g_img_lsb = (body[22] == 0);
    x->min_key = body[26];
    x->max_key = body[27];
    int off = 32 + ((vendor_len + 3) & ~3) + num_fmt * 8;
    for (int r = 0; r < num_roots; r++) {
        uint32_t root = rd32(body + off);
        uint32_t rootvis = rd32(body + off + 32);
        int ndepths = body[off + 39];
        if (r == 0) {
            x->root = root;
            x->root_visual = rootvis;
            x->black_pixel = rd32(body + off + 12);
        }
        off += 40;
        for (int d = 0; d < ndepths; d++) {
            int nv = rd16(body + off + 2);
            off += 8;
            for (int v = 0; v < nv; v++) {
                if (rd32(body + off) == x->root_visual && r == 0) {
                    x->red_mask = rd32(body + off + 8);
                    x->green_mask = rd32(body + off + 12);
                    x->blue_mask = rd32(body + off + 16);
                }
                off += 24;
            }
        }
    }
    if (x->red_mask == 0) {
        x->red_mask = 0xff0000; x->green_mask = 0xff00; x->blue_mask = 0xff;
    }
    return 0;
}

void x_close(XConn *x) {
    if (x->fd >= 0) close(x->fd);
    x->fd = -1;
}

static uint32_t next_id(void) {
    g_rid_next++;
    return g_rid_base | (g_rid_next & g_rid_mask);
}

/* ---------------- requests ---------------- */
static void change_property(XConn *x, uint32_t win, uint32_t prop, uint32_t type,
                            int format, const uint8_t *data, int datalen_items, int item_bytes) {
    int raw = datalen_items * item_bytes;
    int padded = (raw + 3) & ~3;
    uint8_t req[24 + 256];
    if (padded > 256) return;
    memset(req, 0, sizeof req);
    req[0] = 18; req[1] = 0;
    wr16(req + 2, (uint16_t)(6 + padded / 4));
    wr32(req + 4, win);
    wr32(req + 8, prop);
    wr32(req + 12, type);
    req[16] = (uint8_t)format;
    wr32(req + 20, (uint32_t)datalen_items);
    memcpy(req + 24, data, (size_t)raw);
    x_send(x, req, 24 + padded);
}

int x_create_window(XConn *x, int w, int h, const char *title, uint32_t *win_out, uint32_t *gc_out) {
    uint32_t win = next_id(), gc = next_id();
    uint8_t req[40];
    memset(req, 0, sizeof req);
    req[0] = 1; req[1] = 0;
    wr16(req + 2, 10);
    wr32(req + 4, win);
    wr32(req + 8, x->root);
    wr16(req + 12, 80); wr16(req + 14, 60);
    wr16(req + 16, (uint16_t)w); wr16(req + 18, (uint16_t)h);
    wr16(req + 20, 0);
    wr16(req + 22, 1);
    wr32(req + 24, 0);
    wr32(req + 28, 0x802);
    wr32(req + 32, x->black_pixel);
    wr32(req + 36, 0x28003); /* KeyPress|KeyRelease|Exposure|StructureNotify */
    /* the two values push past 40 bytes: build in a bigger scratch */
    uint8_t big[48];
    memcpy(big, req, 32);
    wr32(big + 32, x->black_pixel);
    wr32(big + 36, 0x28003);
    /* fix: first 32 bytes already contain header+fixed fields through value mask at 28 */
    x_send(x, big, 40);
    change_property(x, win, 39, 31, 8, (const uint8_t *)title, (int)strlen(title), 1);
    uint8_t g[16];
    memset(g, 0, sizeof g);
    g[0] = 55;
    wr16(g + 2, 4);
    wr32(g + 4, gc);
    wr32(g + 8, win);
    wr32(g + 12, 0);
    x_send(x, g, 16);
    uint8_t m[8];
    memset(m, 0, sizeof m);
    m[0] = 8;
    wr16(m + 2, 2);
    wr32(m + 4, win);
    x_send(x, m, 8);
    *win_out = win;
    *gc_out = gc;
    return 0;
}

uint32_t x_intern_atom(XConn *x, const char *name) {
    int nl = (int)strlen(name);
    int padded = (nl + 3) & ~3;
    uint8_t req[8 + 256];
    if (padded > 256) return 0;
    memset(req, 0, sizeof req);
    req[0] = 16; req[1] = 0;
    wr16(req + 2, (uint16_t)(2 + padded / 4));
    wr16(req + 4, (uint16_t)nl);
    memcpy(req + 8, name, (size_t)nl);
    if (x_send(x, req, 8 + padded) < 0) return 0;
    uint8_t rep[32];
    int rlen = 0;
    if (wait_reply(x, x->seq, rep, (int)sizeof rep, &rlen) < 0) return 0;
    return rd32(rep + 8);
}

void x_set_wm_delete(XConn *x, uint32_t win, uint32_t *protocols, uint32_t *del_atom) {
    uint32_t pro = x_intern_atom(x, "WM_PROTOCOLS");
    uint32_t del = x_intern_atom(x, "WM_DELETE_WINDOW");
    *protocols = pro;
    *del_atom = del;
    if (!pro || !del) return;
    uint8_t data[4];
    wr32(data, del);
    change_property(x, win, pro, 4, 32, data, 1, 4);
}

void x_keyboard_map(XConn *x, uint8_t keys[10]) {
    static const uint32_t SYMS[10] = {0x77, 0x61, 0x73, 0x64, 0x71, 0xff1b, 0xff52, 0xff51, 0xff54, 0xff53};
    memset(keys, 0, 10);
    int first = x->min_key, count = x->max_key - x->min_key + 1;
    if (count <= 0 || count > 248) return;
    uint8_t req[8];
    memset(req, 0, sizeof req);
    req[0] = 101; req[1] = (uint8_t)first;
    wr16(req + 2, 2);
    req[4] = (uint8_t)count;
    if (x_send(x, req, 8) < 0) return;
    static uint8_t rep[16384];
    int rlen = 0;
    if (wait_reply(x, x->seq, rep, (int)sizeof rep, &rlen) < 0) return;
    if (rlen <= 32) return; /* some servers answer empty: caller falls back */
    int per = rep[1];
    if (per <= 0) return;
    for (int i = 0; i < count; i++) {
        for (int j = 0; j < per; j++) {
            int off = 32 + (i * per + j) * 4;
            if (off + 4 > rlen) break;
            uint32_t sym = rd32(rep + off);
            for (int s = 0; s < 10; s++)
                if (sym == SYMS[s] && keys[s] == 0)
                    keys[s] = (uint8_t)(first + i);
        }
    }
}

/* ---------------- painting ---------------- */
static int shift_for(uint32_t mask) {
    int s = 0;
    while (mask && !(mask & 1)) { mask >>= 1; s++; }
    return s;
}

void x_put_image(XConn *x, uint32_t win, uint32_t gc, const uint16_t *fb, int scale) {
    int win_w = SCR_W * scale, win_h = SCR_H * scale;
    int rowbytes = win_w * 4;
    int rows = (int)sizeof band_buf / rowbytes;
    int max_data = (x->max_req_len - 6) * 4;
    if (max_data >= rowbytes) {
        int by_req = max_data / rowbytes;
        if (by_req < rows) rows = by_req;
    }
    if (rows < 1) rows = 1;
    if (rows > win_h) rows = win_h;
    int rsh = shift_for(x->red_mask), gsh = shift_for(x->green_mask), bsh = shift_for(x->blue_mask);
    for (int band_y = 0; band_y < win_h; band_y += rows) {
        int nrows = rows;
        if (band_y + nrows > win_h) nrows = win_h - band_y;
        int datalen = nrows * rowbytes;
        uint8_t *d = band_buf;
        for (int ry = 0; ry < nrows; ry++) {
            int src_y = (band_y + ry) / scale;
            for (int rx = 0; rx < win_w; rx++) {
                uint16_t c = fb[src_y * SCR_W + rx / scale];
                uint32_t v = ((uint32_t)(red_of(c) * 255 / 31) << rsh) |
                             ((uint32_t)(grn_of(c) * 255 / 31) << gsh) |
                             ((uint32_t)(blu_of(c) * 255 / 31) << bsh);
                int off = ry * rowbytes + rx * 4;
                if (g_img_lsb) wr32(d + off, v);
                else { d[off] = (uint8_t)(v >> 24); d[off + 1] = (uint8_t)(v >> 16); d[off + 2] = (uint8_t)(v >> 8); d[off + 3] = (uint8_t)v; }
            }
        }
        uint8_t req[24];
        memset(req, 0, sizeof req);
        req[0] = 72; req[1] = 2;
        wr16(req + 2, (uint16_t)(6 + datalen / 4));
        wr32(req + 4, win);
        wr32(req + 8, gc);
        wr16(req + 12, (uint16_t)win_w);
        wr16(req + 14, (uint16_t)nrows);
        wr16(req + 16, 0);
        wr16(req + 18, (uint16_t)band_y);
        req[20] = 0;
        req[21] = 24;
        x_send(x, req, 24);
        x_send_raw(x, band_buf, datalen);
    }
}

/* ---------------- read-back (for the self-check) ---------------- */
int x_get_image_points(XConn *x, uint32_t win, int w, int h,
                       const int *px, const int *py, int npts, uint32_t *outvals,
                       uint32_t *red_mask, uint32_t *green_mask, uint32_t *blue_mask, int *img_lsb) {
    uint8_t req[20];
    memset(req, 0, sizeof req);
    req[0] = 73; req[1] = 2;
    wr16(req + 2, 5);
    wr32(req + 4, win);
    wr16(req + 8, 0); wr16(req + 10, 0);
    wr16(req + 12, (uint16_t)w); wr16(req + 14, (uint16_t)h);
    wr32(req + 16, 0xFFFFFFFFu);
    if (x_send(x, req, 20) < 0) return -1;
    uint16_t want_seq = x->seq;
    static uint8_t pkt[16384];
    long data_total = -1;
    /* walk packets until OUR reply header shows up */
    for (;;) {
        if (buf_fill(x, 32, 1) < 32) return -1;
        uint8_t b0 = rbuf[rpos];
        if (b0 == 1 && rd16(rbuf + rpos + 2) == want_seq) {
            data_total = (long)rd32(rbuf + rpos + 4) * 4;
            rpos += 32;
            buf_compact();
            break;
        }
        int plen = 0;
        int kind = next_packet(x, pkt, (int)sizeof pkt, &plen, 1);
        if (kind == 1) { stash_event(x, pkt); continue; }
        if (kind == 2) continue;       /* someone else's reply: drop */
        return -1;                     /* error or trouble */
    }
    /* stream data_total bytes, sampling wanted pixels in order */
    int order[256];
    for (int i = 0; i < npts; i++) order[i] = i;
    for (int i = 0; i < npts; i++)
        for (int j = i + 1; j < npts; j++) {
            long oi2 = (long)py[order[i]] * w * 4 + (long)px[order[i]] * 4;
            long oj2 = (long)py[order[j]] * w * 4 + (long)px[order[j]] * 4;
            if (oj2 < oi2) { int t = order[i]; order[i] = order[j]; order[j] = t; }
        }
    long pos = 0;
    int oi = 0;
    while (data_total > 0) {
        int avail = buf_fill(x, 1, 1);
        if (avail <= 0) return -1;
        long take = avail;
        if (take > data_total) take = data_total;
        while (oi < npts) {
            long target = (long)py[order[oi]] * w * 4 + (long)px[order[oi]] * 4;
            if (target + 4 > pos + take) break;
            int at = (int)(target - pos);
            const uint8_t *b = rbuf + rpos + at;
            if (g_img_lsb) outvals[order[oi]] = rd32(b);
            else outvals[order[oi]] = ((uint32_t)b[0] << 24) | ((uint32_t)b[1] << 16) | ((uint32_t)b[2] << 8) | (uint32_t)b[3];
            oi++;
        }
        rpos += (int)take;
        pos += take;
        data_total -= take;
        buf_compact();
    }
    while (oi < npts) { outvals[order[oi]] = 0; oi++; }
    *red_mask = x->red_mask;
    *green_mask = x->green_mask;
    *blue_mask = x->blue_mask;
    *img_lsb = g_img_lsb;
    return 0;
}

long x_static_bytes(void) {
    return (long)sizeof band_buf + (long)sizeof rbuf;
}
