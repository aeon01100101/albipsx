/* ============================================================
   render.c - the PS1 renderer, in C, with a Z-buffer.
   No malloc anywhere: every byte is a static array whose
   size is fixed in psx.h. Depths are stored as z >> 3 in
   16 bits (150 KB) - plenty at PS1 distances.
   ============================================================ */
#include "psx.h"
#include <string.h>
#include <stdio.h>
#include "trig.h"

Engine E;
int32_t vert_pool[VPOOL][3];
int32_t uv_pool[UPOOL][2];
int32_t face_pool[FPOOL][5];
Mesh meshes[MAX_MESHES];
int mesh_count;
static int vert_used, uv_used, face_used;

typedef struct {
    int32_t x0, y0, x1, y1, x2, y2;
    int32_t cz0, cz1, cz2;
    uint16_t col;
    int32_t tex, shade, mode;
    int32_t s0, s1, s2, u0, v0, u1, v1, u2, v2;
} Tri;

static Tri tri_pool[MAX_TRIS];
static int tri_count;

typedef struct { int32_t x, y, z, u, v, s; } CamVert;
static CamVert cv_scratch[512];

typedef struct {
    int32_t x, y, z, vx, vy, vz, life;
    uint16_t col;
} Part;
static Part parts[MAX_PARTS];
static int part_count;

static uint16_t tex_cache[TEX_SHADE_CACHE][1024];
static int32_t tex_cache_key[TEX_SHADE_CACHE];
static int tex_cache_count;

/* ---------------- colours ---------------- */
uint16_t pack555(int32_t r, int32_t g, int32_t b) {
    return (uint16_t)(clampi(r, 0, 31) * 1024 + clampi(g, 0, 31) * 32 + clampi(b, 0, 31));
}
int32_t red_of(uint16_t c) { return (c / 1024) % 32; }
int32_t grn_of(uint16_t c) { return (c / 32) % 32; }
int32_t blu_of(uint16_t c) { return c % 32; }

uint16_t shade_col(uint16_t c, int32_t s) {
    if (s >= 256) return c;
    if (s <= 0) return 0;
    return pack555(red_of(c) * s / 256, grn_of(c) * s / 256, blu_of(c) * s / 256);
}

uint16_t mix_col(uint16_t c1, uint16_t c2, int32_t t) {
    return pack555(red_of(c1) + (red_of(c2) - red_of(c1)) * t / 256,
                   grn_of(c1) + (grn_of(c2) - grn_of(c1)) * t / 256,
                   blu_of(c1) + (blu_of(c2) - blu_of(c1)) * t / 256);
}

int32_t sin_a(int32_t a) { return SIN_TAB[((a % 256) + 256) % 256]; }
int32_t cos_a(int32_t a) { return COS_TAB[((a % 256) + 256) % 256]; }

static uint32_t rng_next(void) {
    uint32_t x = E.rng;
    x ^= x << 13; x ^= x >> 17; x ^= x << 5;
    E.rng = x;
    return x;
}
static int32_t rng_range(int32_t n) { return (int32_t)(rng_next() % (uint32_t)n); }

/* ---------------- textures ---------------- */
static void build_textures(void) {
    for (int32_t y = 0; y < 32; y++) {
        for (int32_t x = 0; x < 32; x++) {
            int i = y * 32 + x;
            E.tex[0][i] = ((x / 8 + y / 8) % 2 == 0) ? pack555(7, 23, 7) : pack555(5, 18, 5);
            E.tex[1][i] = ((x / 8 + y / 8) % 2 == 0) ? pack555(17, 16, 19) : pack555(12, 11, 15);
            {
                int32_t row = y / 8, off = (row % 2 == 1) ? 8 : 0;
                E.tex[2][i] = (y % 8 == 7 || (x + off) % 16 == 15) ? pack555(20, 18, 16) : pack555(24, 9, 6);
            }
            E.tex[3][i] = (x % 8 == 7) ? pack555(10, 6, 3) : (((x / 8) % 2 == 0) ? pack555(19, 12, 5) : pack555(17, 10, 4));
            {
                int32_t dx = (x % 16) - 8, dy = (y % 16) - 8;
                E.tex[4][i] = (dx * dx + dy * dy < 12) ? pack555(31, 28, 8) : pack555(9, 7, 22);
            }
            E.tex[5][i] = ((x / 8 + y / 8) % 2 == 0) ? pack555(29, 24, 13) : pack555(26, 21, 10);
        }
    }
}

void engine_init(void) {
    memset(&E, 0, sizeof E);
    E.rng = 12345;
    build_textures();
    E.sky_top = pack555(6, 14, 29);
    E.sky_bot = pack555(20, 26, 31);
}

void engine_set_sky(uint16_t top, uint16_t bot) { E.sky_top = top; E.sky_bot = bot; }

void engine_set_camera(int32_t x, int32_t y, int32_t z, int32_t yaw, int32_t pitch) {
    E.cam_x = x; E.cam_y = y; E.cam_z = z;
    E.cam_cos = cos_a(yaw); E.cam_sin = sin_a(yaw);
    E.pit_cos = cos_a(pitch); E.pit_sin = sin_a(pitch);
    int32_t lx = -1024, ly = 3400, lz = 1800;
    int32_t rx = fmul(lx, E.cam_cos) - fmul(lz, E.cam_sin);
    int32_t rz = fmul(lx, E.cam_sin) + fmul(lz, E.cam_cos);
    int32_t ry = fmul(ly, E.pit_cos) - fmul(rz, E.pit_sin);
    int32_t rz2 = fmul(ly, E.pit_sin) + fmul(rz, E.pit_cos);
    E.lx = rx; E.ly = ry; E.lz = rz2;
}

/* ---------------- meshes ---------------- */
void pools_reset(void) {
    mesh_count = 0;
    vert_used = 0;
    uv_used = 0;
    face_used = 0;
}

Mesh *mesh_new(void) {
    if (mesh_count >= MAX_MESHES) return &meshes[0];
    Mesh *m = &meshes[mesh_count++];
    memset(m, 0, sizeof *m);
    m->vstart = vert_used; m->fstart = face_used;
    return m;
}

int mesh_add_vert(Mesh *m, int32_t x, int32_t y, int32_t z, int32_t u, int32_t v) {
    int idx = m->vcount++;
    if (vert_used < VPOOL) {
        vert_pool[vert_used][0] = x; vert_pool[vert_used][1] = y; vert_pool[vert_used][2] = z;
        vert_used++;
    }
    if (uv_used < UPOOL) {
        uv_pool[uv_used][0] = u; uv_pool[uv_used][1] = v;
        uv_used++;
    }
    return idx;
}

void mesh_add_face(Mesh *m, int32_t a, int32_t b, int32_t c, uint16_t col, int32_t tex) {
    if (face_used >= FPOOL) return;
    face_pool[face_used][0] = a; face_pool[face_used][1] = b; face_pool[face_used][2] = c;
    face_pool[face_used][3] = col; face_pool[face_used][4] = tex;
    face_used++;
    m->fcount++;
}

Mesh *mesh_box(int32_t hx, int32_t hy, int32_t hz, const uint16_t cols[6]) {
    static const int32_t V[8][3] = {
        {-1,-1,-1},{1,-1,-1},{1,1,-1},{-1,1,-1},
        {-1,-1,1},{1,-1,1},{1,1,1},{-1,1,1}};
    static const int32_t F[12][4] = {
        {4,5,6,0},{4,6,7,0},{1,0,3,1},{1,3,2,1},
        {0,4,7,2},{0,7,3,2},{5,1,2,3},{5,2,6,3},
        {7,6,2,4},{7,2,3,4},{0,1,5,5},{0,5,4,5}};
    Mesh *m = mesh_new();
    for (int i = 0; i < 8; i++)
        mesh_add_vert(m, V[i][0] * hx, V[i][1] * hy, V[i][2] * hz, 0, 0);
    for (int i = 0; i < 12; i++)
        mesh_add_face(m, F[i][0], F[i][1], F[i][2], cols[F[i][3]], -1);
    return m;
}

Mesh *mesh_pyramid(int32_t hx, int32_t hz, int32_t hgt, const uint16_t cols[5]) {
    static const int32_t F[6][4] = {
        {3,2,4,0},{2,1,4,1},{1,0,4,2},{0,3,4,3},{0,1,2,4},{0,2,3,4}};
    Mesh *m = mesh_new();
    mesh_add_vert(m, -hx, 0, -hz, 0, 0);
    mesh_add_vert(m, hx, 0, -hz, 0, 0);
    mesh_add_vert(m, hx, 0, hz, 0, 0);
    mesh_add_vert(m, -hx, 0, hz, 0, 0);
    mesh_add_vert(m, 0, hgt, 0, 0, 0);
    for (int i = 0; i < 6; i++)
        mesh_add_face(m, F[i][0], F[i][1], F[i][2], cols[F[i][3]], -1);
    return m;
}

Mesh *mesh_gem(int32_t r, int32_t hgt, uint16_t col) {
    static const int32_t F[8][3] = {
        {0,2,1},{0,3,2},{0,4,3},{0,1,4},{5,1,2},{5,2,3},{5,3,4},{5,4,1}};
    static const int32_t VS[6] = {256, 205, 205, 205, 205, 95};
    Mesh *m = mesh_new();
    mesh_add_vert(m, 0, hgt, 0, 0, 0);
    mesh_add_vert(m, r, 0, 0, 0, 0);
    mesh_add_vert(m, 0, 0, r, 0, 0);
    mesh_add_vert(m, -r, 0, 0, 0, 0);
    mesh_add_vert(m, 0, 0, -r, 0, 0);
    mesh_add_vert(m, 0, -hgt, 0, 0, 0);
    for (int i = 0; i < 8; i++)
        mesh_add_face(m, F[i][0], F[i][1], F[i][2], col, -1);
    for (int i = 0; i < 6; i++) m->vsh[i] = VS[i];
    m->has_vsh = 1;
    return m;
}

Mesh *mesh_ground(int32_t half, int32_t texid, uint16_t col) {
    Mesh *m = mesh_new();
    m->ground = 1;
    const int32_t cells = 8, step = (half * 2) / cells;
    for (int32_t iz = 0; iz <= cells; iz++)
        for (int32_t ix = 0; ix <= cells; ix++)
            mesh_add_vert(m, -half + ix * step, 0, -half + iz * step, ix * 2048, iz * 2048);
    for (int32_t iz = 0; iz < cells; iz++) {
        for (int32_t ix = 0; ix < cells; ix++) {
            int32_t a = iz * (cells + 1) + ix, b = a + 1, d = a + cells + 1, c = d + 1;
            mesh_add_face(m, a, c, b, col, texid);
            mesh_add_face(m, a, d, c, col, texid);
        }
    }
    return m;
}

/* ---------------- framebuffer helpers ---------------- */
void fill_rect(int32_t x0, int32_t y0, int32_t x1, int32_t y1, uint16_t col) {
    x0 = clampi(x0, 0, SCR_W - 1); x1 = clampi(x1, 0, SCR_W - 1);
    y0 = clampi(y0, 0, SCR_H - 1); y1 = clampi(y1, 0, SCR_H - 1);
    for (int32_t y = y0; y <= y1; y++)
        for (int32_t x = x0; x <= x1; x++)
            E.fb[y * SCR_W + x] = col;
}

static void fill_rect_z(int32_t x0, int32_t y0, int32_t x1, int32_t y1, int32_t z, uint16_t col) {
    x0 = clampi(x0, 0, SCR_W - 1); x1 = clampi(x1, 0, SCR_W - 1);
    y0 = clampi(y0, 0, SCR_H - 1); y1 = clampi(y1, 0, SCR_H - 1);
    uint16_t z16 = (uint16_t)clampi(z >> 3, 0, 65535);
    for (int32_t y = y0; y <= y1; y++)
        for (int32_t x = x0; x <= x1; x++) {
            int idx = y * SCR_W + x;
            if (z16 < E.zb[idx]) E.fb[idx] = col;
        }
}

static void clear_sky(void) {
    const int32_t bands = 16, bh = SCR_H / bands;
    for (int32_t band = 0; band < bands; band++) {
        uint16_t col = mix_col(E.sky_top, E.sky_bot, band * 256 / (bands - 1));
        for (int32_t y = band * bh; y < (band + 1) * bh; y++)
            for (int32_t x = 0; x < SCR_W; x++)
                E.fb[y * SCR_W + x] = col;
    }
    for (int i = 0; i < SCR_PIX; i++) E.zb[i] = 0xFFFF;
}

int project_point(int32_t wx, int32_t wy, int32_t wz, int32_t *sx, int32_t *sy, int32_t *czout) {
    int32_t dx = wx - E.cam_x, dy = wy - E.cam_y, dz = wz - E.cam_z;
    int32_t cx = fmul(dx, E.cam_cos) - fmul(dz, E.cam_sin);
    int32_t cz = fmul(dx, E.cam_sin) + fmul(dz, E.cam_cos);
    int32_t cy = fmul(dy, E.pit_cos) - fmul(cz, E.pit_sin);
    int32_t cz2 = fmul(dy, E.pit_sin) + fmul(cz, E.pit_cos);
    cx = (cx / SNAP_Q) * SNAP_Q;
    cy = (cy / SNAP_Q) * SNAP_Q;
    cz2 = (cz2 / SNAP_Q) * SNAP_Q;
    if (cz2 < NEAR_Z) return 0;
    *sx = 160 + (cx * FOCAL) / cz2;
    *sy = 120 - (cy * FOCAL) / cz2;
    *czout = cz2;
    return 1;
}

/* ---------------- triangle painters, with depth ---------------- */
static int64_t edgef(int32_t ax, int32_t ay, int32_t bx, int32_t by, int32_t px, int32_t py) {
    return (int64_t)(px - ax) * (by - ay) - (int64_t)(py - ay) * (bx - ax);
}

typedef struct {
    int64_t area, w0, w1, w2;
    int64_t dwdx0, dwdy0, dwdx1, dwdy1, dwdx2, dwdy2;
    int64_t znum, dzdx, dzdy;
    int32_t minx, maxx, miny, maxy;
    int ok;
} Prep;

static Prep prep_tri(const Tri *t) {
    Prep p;
    memset(&p, 0, sizeof p);
    int64_t area = edgef(t->x0, t->y0, t->x1, t->y1, t->x2, t->y2);
    if (area == 0) return p;
    p.minx = clampi(mini32(t->x0, mini32(t->x1, t->x2)), 0, SCR_W - 1);
    p.maxx = clampi(maxi32(t->x0, maxi32(t->x1, t->x2)), 0, SCR_W - 1);
    p.miny = clampi(mini32(t->y0, mini32(t->y1, t->y2)), 0, SCR_H - 1);
    p.maxy = clampi(maxi32(t->y0, maxi32(t->y1, t->y2)), 0, SCR_H - 1);
    p.dwdx0 = t->y2 - t->y1; p.dwdy0 = t->x1 - t->x2;
    p.dwdx1 = t->y0 - t->y2; p.dwdy1 = t->x2 - t->x0;
    p.dwdx2 = t->y1 - t->y0; p.dwdy2 = t->x0 - t->x1;
    p.w0 = edgef(t->x1, t->y1, t->x2, t->y2, p.minx, p.miny);
    p.w1 = edgef(t->x2, t->y2, t->x0, t->y0, p.minx, p.miny);
    p.w2 = edgef(t->x0, t->y0, t->x1, t->y1, p.minx, p.miny);
    if (area < 0) {
        area = -area;
        p.w0 = -p.w0; p.w1 = -p.w1; p.w2 = -p.w2;
        p.dwdx0 = -p.dwdx0; p.dwdy0 = -p.dwdy0;
        p.dwdx1 = -p.dwdx1; p.dwdy1 = -p.dwdy1;
        p.dwdx2 = -p.dwdx2; p.dwdy2 = -p.dwdy2;
    }
    p.area = area;
    p.znum = p.w0 * t->cz0 + p.w1 * t->cz1 + p.w2 * t->cz2;
    p.dzdx = (int64_t)t->cz0 * p.dwdx0 + (int64_t)t->cz1 * p.dwdx1 + (int64_t)t->cz2 * p.dwdx2;
    p.dzdy = (int64_t)t->cz0 * p.dwdy0 + (int64_t)t->cz1 * p.dwdy1 + (int64_t)t->cz2 * p.dwdy2;
    p.ok = 1;
    return p;
}

static int raster_flat(const Tri *t) {
    Prep p = prep_tri(t);
    if (!p.ok) return 0;
    int count = 0;
    int64_t w0 = p.w0, w1 = p.w1, w2 = p.w2, znum = p.znum;
    for (int32_t y = p.miny; y <= p.maxy; y++) {
        int64_t w0r = w0, w1r = w1, w2r = w2, zrow = znum;
        int base = y * SCR_W;
        for (int32_t x = p.minx; x <= p.maxx; x++) {
            if (w0r >= 0 && w1r >= 0 && w2r >= 0) {
                uint16_t z16 = (uint16_t)clampi((int32_t)(zrow / p.area) >> 3, 0, 65535);
                int idx = base + x;
                if (z16 < E.zb[idx]) {
                    E.zb[idx] = z16;
                    E.fb[idx] = t->col;
                    count++;
                }
            }
            w0r += p.dwdx0; w1r += p.dwdx1; w2r += p.dwdx2; zrow += p.dzdx;
        }
        w0 += p.dwdy0; w1 += p.dwdy1; w2 += p.dwdy2; znum += p.dzdy;
    }
    return count;
}

static int raster_gouraud(const Tri *t) {
    Prep p = prep_tri(t);
    if (!p.ok) return 0;
    int64_t snum = p.w0 * t->s0 + p.w1 * t->s1 + p.w2 * t->s2;
    int64_t dsdx = (int64_t)t->s0 * p.dwdx0 + (int64_t)t->s1 * p.dwdx1 + (int64_t)t->s2 * p.dwdx2;
    int64_t dsdy = (int64_t)t->s0 * p.dwdy0 + (int64_t)t->s1 * p.dwdy1 + (int64_t)t->s2 * p.dwdy2;
    int count = 0;
    int64_t w0 = p.w0, w1 = p.w1, w2 = p.w2, znum = p.znum;
    for (int32_t y = p.miny; y <= p.maxy; y++) {
        int64_t w0r = w0, w1r = w1, w2r = w2, zrow = znum, srow = snum;
        int base = y * SCR_W;
        for (int32_t x = p.minx; x <= p.maxx; x++) {
            if (w0r >= 0 && w1r >= 0 && w2r >= 0) {
                uint16_t z16 = (uint16_t)clampi((int32_t)(zrow / p.area) >> 3, 0, 65535);
                int idx = base + x;
                if (z16 < E.zb[idx]) {
                    E.zb[idx] = z16;
                    E.fb[idx] = shade_col(t->col, clampi((int32_t)(srow / p.area), 0, 256));
                    count++;
                }
            }
            w0r += p.dwdx0; w1r += p.dwdx1; w2r += p.dwdx2; zrow += p.dzdx; srow += dsdx;
        }
        w0 += p.dwdy0; w1 += p.dwdy1; w2 += p.dwdy2; znum += p.dzdy; snum += dsdy;
    }
    return count;
}

static const uint16_t *shaded_tex(int32_t texid, int32_t shade) {
    if (shade == 256) return E.tex[texid];
    int32_t key = texid * 512 + shade;
    for (int i = 0; i < tex_cache_count; i++)
        if (tex_cache_key[i] == key) return tex_cache[i];
    if (tex_cache_count >= TEX_SHADE_CACHE) return E.tex[texid];
    int slot = tex_cache_count++;
    tex_cache_key[slot] = key;
    for (int i = 0; i < 1024; i++) tex_cache[slot][i] = shade_col(E.tex[texid][i], shade);
    return tex_cache[slot];
}

static int raster_tex(const Tri *t) {
    Prep p = prep_tri(t);
    if (!p.ok) return 0;
    int64_t unum = p.w0 * t->u0 + p.w1 * t->u1 + p.w2 * t->u2;
    int64_t dudx = (int64_t)t->u0 * p.dwdx0 + (int64_t)t->u1 * p.dwdx1 + (int64_t)t->u2 * p.dwdx2;
    int64_t dudy = (int64_t)t->u0 * p.dwdy0 + (int64_t)t->u1 * p.dwdy1 + (int64_t)t->u2 * p.dwdy2;
    int64_t vnum = p.w0 * t->v0 + p.w1 * t->v1 + p.w2 * t->v2;
    int64_t dvdx = (int64_t)t->v0 * p.dwdx0 + (int64_t)t->v1 * p.dwdx1 + (int64_t)t->v2 * p.dwdx2;
    int64_t dvdy = (int64_t)t->v0 * p.dwdy0 + (int64_t)t->v1 * p.dwdy1 + (int64_t)t->v2 * p.dwdy2;
    const uint16_t *tex = shaded_tex(t->tex, t->shade);
    int count = 0;
    int64_t w0 = p.w0, w1 = p.w1, w2 = p.w2, znum = p.znum;
    for (int32_t y = p.miny; y <= p.maxy; y++) {
        int64_t w0r = w0, w1r = w1, w2r = w2, zrow = znum, urow = unum, vrow = vnum;
        int base = y * SCR_W;
        for (int32_t x = p.minx; x <= p.maxx; x++) {
            if (w0r >= 0 && w1r >= 0 && w2r >= 0) {
                uint16_t z16 = (uint16_t)clampi((int32_t)(zrow / p.area) >> 3, 0, 65535);
                int idx = base + x;
                if (z16 < E.zb[idx]) {
                    E.zb[idx] = z16;
                    int64_t uu = urow / p.area, vv = vrow / p.area;
                    int32_t tx = (int32_t)((uu / 128) % 32); if (tx < 0) tx += 32;
                    int32_t ty = (int32_t)((vv / 128) % 32); if (ty < 0) ty += 32;
                    E.fb[idx] = tex[ty * 32 + tx];
                    count++;
                }
            }
            w0r += p.dwdx0; w1r += p.dwdx1; w2r += p.dwdx2;
            zrow += p.dzdx; urow += dudx; vrow += dvdx;
        }
        w0 += p.dwdy0; w1 += p.dwdy1; w2 += p.dwdy2;
        znum += p.dzdy; unum += dudy; vnum += dvdy;
    }
    return count;
}

/* ---------------- pipeline: world -> camera -> clip -> paint ---------------- */
static int64_t isqrt64(int64_t v) {
    if (v <= 0) return 0;
    int64_t r = v / 2 + 1;
    for (int i = 0; i < 40; i++) {
        int64_t nr = (r + v / r) / 2;
        if (nr >= r) break;
        r = nr;
    }
    while ((r + 1) * (r + 1) <= v) r++;
    while (r * r > v) r--;
    return r;
}

static int clip_near(CamVert *poly, int n, CamVert *out) {
    int m = 0;
    for (int i = 0; i < n; i++) {
        CamVert cur = poly[i], nxt = poly[(i + 1) % n];
        int cur_in = cur.z >= NEAR_Z, nxt_in = nxt.z >= NEAR_Z;
        if (cur_in) out[m++] = cur;
        if (cur_in != nxt_in) {
            int64_t tnum = (int64_t)(NEAR_Z - cur.z), tden = (int64_t)(nxt.z - cur.z);
            CamVert mid;
#define LERP(f) (cur.f + (int32_t)(((int64_t)(nxt.f - cur.f) * tnum) / tden))
            mid.x = LERP(x); mid.y = LERP(y); mid.z = NEAR_Z;
            mid.u = LERP(u); mid.v = LERP(v); mid.s = LERP(s);
#undef LERP
            out[m++] = mid;
        }
    }
    return m;
}

static void emit_tri(CamVert p, CamVert q, CamVert r, uint16_t col, int32_t texid, int32_t shade, int gouraud) {
    if (tri_count >= MAX_TRIS) return;
    Tri *t = &tri_pool[tri_count++];
    t->x0 = 160 + (p.x * FOCAL) / p.z; t->y0 = 120 - (p.y * FOCAL) / p.z;
    t->x1 = 160 + (q.x * FOCAL) / q.z; t->y1 = 120 - (q.y * FOCAL) / q.z;
    t->x2 = 160 + (r.x * FOCAL) / r.z; t->y2 = 120 - (r.y * FOCAL) / r.z;
    t->cz0 = p.z; t->cz1 = q.z; t->cz2 = r.z;
    t->col = col; t->tex = texid; t->shade = shade;
    t->s0 = 256; t->s1 = 256; t->s2 = 256;
    t->u0 = t->v0 = t->u1 = t->v1 = t->u2 = t->v2 = 0;
    if (texid >= 0) {
        t->mode = 2;
        t->u0 = p.u; t->v0 = p.v; t->u1 = q.u; t->v1 = q.v; t->u2 = r.u; t->v2 = r.v;
    } else if (gouraud) {
        t->mode = 1;
        t->s0 = shade * p.s / 256; t->s1 = shade * q.s / 256; t->s2 = shade * r.s / 256;
    } else {
        t->mode = 0;
        t->col = shade_col(col, shade);
    }
}

static void add_thing(Thing *th) {
    Mesh *mesh = th->mesh;
    int32_t ca = cos_a(th->ry), sa = sin_a(th->ry);
    int n = mesh->vcount;
    if (n > 512) n = 512;
    for (int i = 0; i < n; i++) {
        int32_t vx = vert_pool[mesh->vstart + i][0];
        int32_t vy = vert_pool[mesh->vstart + i][1];
        int32_t vz = vert_pool[mesh->vstart + i][2];
        int32_t rx = fmul(vx, ca) - fmul(vz, sa);
        int32_t rz = fmul(vx, sa) + fmul(vz, ca);
        int32_t wx = rx + th->x, wy = vy + th->y, wz = rz + th->z;
        int32_t dx = wx - E.cam_x, dy = wy - E.cam_y, dz = wz - E.cam_z;
        int32_t cx = fmul(dx, E.cam_cos) - fmul(dz, E.cam_sin);
        int32_t cz = fmul(dx, E.cam_sin) + fmul(dz, E.cam_cos);
        int32_t cy = fmul(dy, E.pit_cos) - fmul(cz, E.pit_sin);
        int32_t cz2 = fmul(dy, E.pit_sin) + fmul(cz, E.pit_cos);
        CamVert *cv = &cv_scratch[i];
        cv->x = (cx / SNAP_Q) * SNAP_Q;
        cv->y = (cy / SNAP_Q) * SNAP_Q;
        cv->z = (cz2 / SNAP_Q) * SNAP_Q;
        cv->u = uv_pool[mesh->vstart + i][0];
        cv->v = uv_pool[mesh->vstart + i][1];
        cv->s = mesh->has_vsh ? mesh->vsh[i] : 256;
    }
    for (int fi = 0; fi < mesh->fcount; fi++) {
        int32_t *f = face_pool[mesh->fstart + fi];
        E.tris_seen++;
        CamVert va = cv_scratch[f[0]], vb = cv_scratch[f[1]], vc = cv_scratch[f[2]];
        int64_t ux = vb.x - va.x, uy = vb.y - va.y, uz = vb.z - va.z;
        int64_t vx = vc.x - va.x, vy = vc.y - va.y, vz = vc.z - va.z;
        int64_t nx = uy * vz - uz * vy;
        int64_t ny = uz * vx - ux * vz;
        int64_t nz = ux * vy - uy * vx;
        int64_t mx = (va.x + vb.x + vc.x) / 3;
        int64_t my = (va.y + vb.y + vc.y) / 3;
        int64_t mz = (va.z + vb.z + vc.z) / 3;
        if (nx * mx + ny * my + nz * mz >= 0) continue;
        int64_t nlen = isqrt64(nx * nx + ny * ny + nz * nz);
        int32_t shade = 176;
        if (nlen > 0) {
            int64_t ndot = (nx * E.lx + ny * E.ly + nz * E.lz) * 4096 / (nlen * 3981);
            shade = 176 + (int32_t)(ndot * 80 / 4096);
        }
        shade = clampi(shade, 96, 256);
        shade = (shade / 16) * 16;
        CamVert poly[3] = {va, vb, vc}, out[4];
        int m = clip_near(poly, 3, out);
        if (m < 3) continue;
        emit_tri(out[0], out[1], out[2], (uint16_t)f[3], f[4], shade, mesh->has_vsh);
        if (m == 4) emit_tri(out[0], out[2], out[3], (uint16_t)f[3], f[4], shade, mesh->has_vsh);
    }
}

/* ---------------- sparkles ---------------- */
void spawn_sparkle(int32_t x, int32_t y, int32_t z, uint16_t col, int n) {
    for (int i = 0; i < n && part_count < MAX_PARTS; i++) {
        Part *p = &parts[part_count++];
        p->x = x; p->y = y; p->z = z;
        p->vx = rng_range(641) - 320;
        p->vy = 500 + rng_range(1000);
        p->vz = rng_range(641) - 320;
        p->life = 18 + rng_range(18);
        p->col = col;
    }
}

void step_parts(void) {
    int m = 0;
    for (int i = 0; i < part_count; i++) {
        Part p = parts[i];
        p.x += p.vx / 3; p.y += p.vy / 3; p.z += p.vz / 3;
        p.vy -= 30; p.life--;
        if (p.life > 0 && p.y >= 0) parts[m++] = p;
    }
    part_count = m;
}

int parts_alive(void) { return part_count; }

static void draw_parts(void) {
    for (int i = 0; i < part_count; i++) {
        Part *p = &parts[i];
        int32_t sx, sy, cz;
        if (!project_point(p->x, p->y, p->z, &sx, &sy, &cz)) continue;
        int32_t sz = clampi((FOCAL * 260) / cz, 1, 7), half = sz / 2;
        fill_rect_z(sx - half, sy - half, sx + half, sy + half, cz, p->col);
    }
}

int engine_render(Thing *things, int nthings) {
    E.tris_seen = 0; E.tris_drawn = 0; E.pixels = 0;
    tri_count = 0;
    tex_cache_count = 0;
    for (int i = 0; i < nthings; i++) {
        add_thing(&things[i]);
        if (tri_count >= MAX_TRIS) break;
    }
    clear_sky();
    for (int i = 0; i < tri_count; i++) {
        Tri *t = &tri_pool[i];
        if (t->mode == 2) E.pixels += raster_tex(t);
        else if (t->mode == 1) E.pixels += raster_gouraud(t);
        else E.pixels += raster_flat(t);
        E.tris_drawn++;
    }
    draw_parts();
    E.tick++;
    return E.tris_drawn;
}

int64_t fb_checksum(void) {
    int64_t total = 0;
    for (int i = 0; i < SCR_PIX; i += 997) total += E.fb[i];
    return total;
}

long render_static_bytes(void) {
    return (long)sizeof tri_pool + (long)sizeof parts + (long)sizeof tex_cache
         + (long)sizeof cv_scratch;
}

/* ---------------- tiny letters (3 wide; M, N, W are 5 wide) ---------------- */
static const char *const FONT[128][5] = {
    ['A'] = {"010","101","111","101","101"}, ['B'] = {"110","101","110","101","110"},
    ['C'] = {"011","100","100","100","011"}, ['D'] = {"110","101","101","101","110"},
    ['E'] = {"111","100","110","100","111"}, ['F'] = {"111","100","110","100","100"},
    ['G'] = {"011","100","101","101","011"}, ['H'] = {"101","101","111","101","101"},
    ['I'] = {"111","010","010","010","111"}, ['J'] = {"001","001","001","101","010"},
    ['K'] = {"101","101","110","101","101"}, ['L'] = {"100","100","100","100","111"},
    ['M'] = {"10001","11011","10101","10001","10001"},
    ['N'] = {"10001","11001","10101","10011","10001"},
    ['O'] = {"010","101","101","101","010"}, ['P'] = {"110","101","110","100","100"},
    ['Q'] = {"010","101","101","010","001"}, ['R'] = {"110","101","110","101","101"},
    ['S'] = {"011","100","010","001","110"}, ['T'] = {"111","010","010","010","010"},
    ['U'] = {"101","101","101","101","111"}, ['V'] = {"101","101","101","101","010"},
    ['W'] = {"10001","10001","10001","10101","01010"},
    ['X'] = {"101","101","010","101","101"}, ['Y'] = {"101","101","010","010","010"},
    ['Z'] = {"111","001","010","100","111"},
    ['0'] = {"010","101","101","101","010"}, ['1'] = {"010","110","010","010","111"},
    ['2'] = {"110","001","010","100","111"}, ['3'] = {"110","001","010","001","110"},
    ['4'] = {"101","101","111","001","001"}, ['5'] = {"111","100","110","001","110"},
    ['6'] = {"011","100","110","101","010"}, ['7'] = {"111","001","001","010","010"},
    ['8'] = {"010","101","010","101","010"}, ['9'] = {"010","101","011","001","110"},
    [' '] = {"000","000","000","000","000"}, ['.'] = {"000","000","000","000","010"},
    ['!'] = {"010","010","010","000","010"}, [':'] = {"000","010","000","010","000"},
    ['-'] = {"000","000","111","000","000"}, ['+'] = {"000","010","111","010","000"},
    ['/'] = {"001","001","010","100","100"},
};

void draw_text(int32_t x, int32_t y, const char *text, uint16_t col, int32_t scale) {
    int32_t cx = x;
    for (const char *p = text; *p; p++) {
        char ch = *p;
        if (ch >= 'a' && ch <= 'z') ch = (char)(ch - 'a' + 'A');
        const char *const *g = FONT[(unsigned char)ch];
        if (g[0]) {
            int32_t gw = (int32_t)strlen(g[0]);
            for (int32_t row = 0; row < 5; row++) {
                const char *bits = g[row];
                for (int32_t ci = 0; ci < gw; ci++) {
                    if (bits[ci] == '1')
                        fill_rect(cx + ci * scale, y + row * scale,
                                  cx + ci * scale + scale - 1, y + row * scale + scale - 1, col);
                }
            }
            cx += (gw + 1) * scale;
        } else {
            cx += 4 * scale;
        }
    }
}

/* ---------------- CORDIC atan2, fixed point ----------------
   Returns the direction of (dx, dz) in 1/1024 brads, where
   direction A means forward = (sin A, cos A). No floats. */
int32_t atan2_brads(int32_t dy, int32_t dx) {
    /* standard math frame: X = dx (cos part), Y = dy (sin part) */
    int64_t x = dx, y = dy;
    if (x == 0 && y == 0) return 0;
    int32_t base = 0;
    if (x < 0) { x = -x; y = -y; base = 128 * 1024; }
    x <<= 10; y <<= 10;
    int64_t z = 0;
    for (int i = 0; i < 16; i++) {
        int64_t xn, yn;
        if (y > 0) {
            xn = x + (y >> i);
            yn = y - (x >> i);
            z += ATAN_TAB[i];
        } else {
            xn = x - (y >> i);
            yn = y + (x >> i);
            z -= ATAN_TAB[i];
        }
        x = xn; y = yn;
    }
    int32_t out = base + (int32_t)z;
    out %= (256 * 1024);
    if (out < 0) out += 256 * 1024;
    return out;
}

/* ---------------- picture saving ---------------- */
int save_ppm(const char *path) {
    FILE *f = fopen(path, "w");
    if (!f) return -1;
    fprintf(f, "P3\n320 240\n31\n");
    for (int y = 0; y < SCR_H; y++) {
        for (int x = 0; x < SCR_W; x++) {
            uint16_t c = E.fb[y * SCR_W + x];
            fprintf(f, "%d %d %d ", red_of(c), grn_of(c), blu_of(c));
        }
        fputc('\n', f);
    }
    fclose(f);
    return 0;
}

/* PNG with stored (uncompressed) DEFLATE blocks: a valid PNG
   written with nothing but a CRC and an Adler checksum. */
static uint32_t crc_tab[256];
static int crc_ready = 0;
static uint32_t png_crc(const uint8_t *buf, size_t len) {
    if (!crc_ready) {
        for (uint32_t n = 0; n < 256; n++) {
            uint32_t c = n;
            for (int k = 0; k < 8; k++) c = (c & 1) ? (0xEDB88320u ^ (c >> 1)) : (c >> 1);
            crc_tab[n] = c;
        }
        crc_ready = 1;
    }
    uint32_t c = 0xFFFFFFFFu;
    for (size_t i = 0; i < len; i++) c = crc_tab[(c ^ buf[i]) & 0xFF] ^ (c >> 8);
    return c ^ 0xFFFFFFFFu;
}

static void put_u32be(FILE *f, uint32_t v) {
    fputc((int)(v >> 24) & 0xFF, f); fputc((int)(v >> 16) & 0xFF, f);
    fputc((int)(v >> 8) & 0xFF, f); fputc((int)v & 0xFF, f);
}

/* simpler: build each chunk's data in a static scratch is too
   big for IDAT; stream it while accumulating CRC by hand. */
typedef struct { uint32_t c; } CrcAcc;
static void crc_acc(CrcAcc *a, const uint8_t *buf, size_t len) {
    if (!crc_ready) (void)png_crc(buf, 0);
    uint32_t c = a->c;
    for (size_t i = 0; i < len; i++) c = crc_tab[(c ^ buf[i]) & 0xFF] ^ (c >> 8);
    a->c = c;
}

static void chunk_begin(FILE *f, const char *type, uint32_t len, CrcAcc *acc) {
    put_u32be(f, len);
    fwrite(type, 1, 4, f);
    acc->c = 0xFFFFFFFFu;
    crc_acc(acc, (const uint8_t *)type, 4);
}
static void chunk_data(FILE *f, CrcAcc *acc, const uint8_t *buf, size_t len) {
    fwrite(buf, 1, len, f);
    crc_acc(acc, buf, len);
}
static void chunk_end(FILE *f, CrcAcc *acc) {
    put_u32be(f, acc->c ^ 0xFFFFFFFFu);
}

int save_png(const char *path) {
    static const uint8_t SIG[8] = {137, 80, 78, 71, 13, 10, 26, 10};
    static uint8_t chunkbuf[16 * (1 + SCR_W * 3)];
    FILE *f = fopen(path, "wb");
    if (!f) return -1;
    fwrite(SIG, 1, 8, f);
    {
        uint8_t ihdr[13];
        ihdr[0] = 0; ihdr[1] = 0; ihdr[2] = (uint8_t)(SCR_W >> 8); ihdr[3] = (uint8_t)SCR_W;
        ihdr[4] = 0; ihdr[5] = 0; ihdr[6] = (uint8_t)(SCR_H >> 8); ihdr[7] = (uint8_t)SCR_H;
        ihdr[8] = 8; ihdr[9] = 2; ihdr[10] = 0; ihdr[11] = 0; ihdr[12] = 0;
        CrcAcc acc;
        chunk_begin(f, "IHDR", 13, &acc);
        chunk_data(f, &acc, ihdr, 13);
        chunk_end(f, &acc);
    }
    const size_t rowbytes = 1 + SCR_W * 3;
    const size_t rawlen = (size_t)SCR_H * rowbytes;
    /* adler over the whole raw stream, built chunk by chunk */
    uint32_t adler_a = 1, adler_b = 0;
    uint32_t blocks = 0;
    {
        size_t done = 0;
        while (done < rawlen) {
            size_t n = rawlen - done;
            if (n > sizeof chunkbuf) n = sizeof chunkbuf;
            blocks++;
            done += n;
        }
    }
    (void)rowbytes;
    /* build + adler in one pass over rows, chunk by chunk */
    adler_a = 1; adler_b = 0;
    {
        size_t filled = 0;
        for (int y = 0; y < SCR_H; y++) {
            chunkbuf[filled++] = 0;
            adler_a = (adler_a + 0) % 65521; adler_b = (adler_b + adler_a) % 65521;
            for (int x = 0; x < SCR_W; x++) {
                uint16_t c = E.fb[y * SCR_W + x];
                uint8_t r8 = (uint8_t)(red_of(c) * 255 / 31);
                uint8_t g8 = (uint8_t)(grn_of(c) * 255 / 31);
                uint8_t b8 = (uint8_t)(blu_of(c) * 255 / 31);
                chunkbuf[filled++] = r8; chunkbuf[filled++] = g8; chunkbuf[filled++] = b8;
                adler_a = (adler_a + r8) % 65521; adler_b = (adler_b + adler_a) % 65521;
                adler_a = (adler_a + g8) % 65521; adler_b = (adler_b + adler_a) % 65521;
                adler_a = (adler_a + b8) % 65521; adler_b = (adler_b + adler_a) % 65521;
            }
            if (filled == sizeof chunkbuf || y == SCR_H - 1) {
                filled = 0; /* data regenerated below for writing; adler done */
            }
        }
    }
    uint32_t adler = (adler_b << 16) | adler_a;
    uint32_t idat_len = 2 + blocks * 5 + (uint32_t)rawlen + 4;
    {
        CrcAcc acc;
        chunk_begin(f, "IDAT", idat_len, &acc);
        uint8_t zhdr[2] = {0x78, 0x01};
        chunk_data(f, &acc, zhdr, 2);
        size_t sent = 0;
        int y = 0;
        while (sent < rawlen) {
            size_t filled = 0;
            while (filled < sizeof chunkbuf && y < SCR_H) {
                chunkbuf[filled++] = 0;
                for (int x = 0; x < SCR_W; x++) {
                    uint16_t c = E.fb[y * SCR_W + x];
                    chunkbuf[filled++] = (uint8_t)(red_of(c) * 255 / 31);
                    chunkbuf[filled++] = (uint8_t)(grn_of(c) * 255 / 31);
                    chunkbuf[filled++] = (uint8_t)(blu_of(c) * 255 / 31);
                }
                y++;
            }
            int last = (sent + filled == rawlen);
            uint8_t bh[5];
            bh[0] = (uint8_t)(last ? 1 : 0);
            bh[1] = (uint8_t)(filled & 0xFF); bh[2] = (uint8_t)((filled >> 8) & 0xFF);
            uint16_t nlen = (uint16_t)~(uint16_t)filled;
            bh[3] = (uint8_t)(nlen & 0xFF); bh[4] = (uint8_t)(nlen >> 8);
            chunk_data(f, &acc, bh, 5);
            chunk_data(f, &acc, chunkbuf, filled);
            sent += filled;
        }
        uint8_t ab[4] = {(uint8_t)(adler >> 24), (uint8_t)(adler >> 16), (uint8_t)(adler >> 8), (uint8_t)adler};
        chunk_data(f, &acc, ab, 4);
        chunk_end(f, &acc);
    }
    {
        CrcAcc acc;
        chunk_begin(f, "IEND", 0, &acc);
        chunk_end(f, &acc);
    }
    fclose(f);
    return 0;
}

/* ---------------- RAM accounting ---------------- */
long statics_total_bytes(void) {
    return (long)sizeof E + (long)sizeof W
         + (long)sizeof vert_pool + (long)sizeof face_pool + (long)sizeof uv_pool
         + (long)sizeof meshes + render_static_bytes();
}

/* ============================================================
   selftest - proof the z-buffer works, the floor never
   tears, and the whole thing fits a PS1. albipsx --selftest
   ============================================================ */
static uint16_t fb_copy[SCR_PIX];

static Tri test_tri(int32_t z, uint16_t col) {
    Tri t;
    memset(&t, 0, sizeof t);
    t.x0 = 20; t.y0 = 20; t.x1 = 300; t.y1 = 40; t.x2 = 160; t.y2 = 220;
    t.cz0 = z; t.cz1 = z; t.cz2 = z;
    t.col = col; t.tex = -1; t.shade = 256;
    t.s0 = 256; t.s1 = 256; t.s2 = 256;
    return t;
}

static void build_demo_world(void) {
    w_start("GEM HUNT", 1);
    w_make_sky("sky");
    w_make_ground("grass");
    w_add_tree(-4, 3);
    w_add_house(6, 8, "orange");
    w_add_mountain(8, -7, "gray");
    w_add_box(2, 2, "blue");
    w_add_gem(0, -2);
    w_add_gem(3, 3);
    w_put_player(0, -6, "red");
}

int run_selftest(void) {
    int pass = 0, fail = 0;
#define CHECK(name, ok) do { \
        if (ok) { pass++; printf("PASS %s\n", name); } \
        else { fail++; printf("FAIL %s\n", name); } \
    } while (0)

    uint16_t red = pack555(31, 0, 0), green = pack555(0, 31, 0);
    engine_init();
    pools_reset();

    CHECK("z-buffer exists (320x240 depths)", sizeof E.zb == SCR_PIX * 2);
    CHECK("framebuffer exists", sizeof E.fb == SCR_PIX * 2);

    Tri far_t = test_tri(9000, red), near_t = test_tri(5000, green);
    clear_sky();
    Tri a = far_t, b = near_t;
    raster_flat(&a);
    raster_flat(&b);
    CHECK("near triangle wins when drawn last", E.fb[100 * SCR_W + 160] == green);
    CHECK("z-buffer stored the near depth", E.zb[100 * SCR_W + 160] == (5000 >> 3));
    memcpy(fb_copy, E.fb, sizeof fb_copy);

    clear_sky();
    a = near_t; b = far_t;
    raster_flat(&a);
    raster_flat(&b);
    CHECK("near triangle wins when drawn first", E.fb[100 * SCR_W + 160] == green);
    CHECK("both draw orders give the identical picture",
          memcmp(fb_copy, E.fb, sizeof fb_copy) == 0);

    clear_sky();
    Tri ramp_r, ramp_g;
    memset(&ramp_r, 0, sizeof ramp_r);
    memset(&ramp_g, 0, sizeof ramp_g);
    ramp_r.x0 = 20; ramp_r.y0 = 20; ramp_r.x1 = 300; ramp_r.y1 = 20; ramp_r.x2 = 160; ramp_r.y2 = 230;
    ramp_r.cz0 = 3000; ramp_r.cz1 = 13000; ramp_r.cz2 = 8000;
    ramp_r.col = red; ramp_r.tex = -1; ramp_r.shade = 256; ramp_r.s0 = ramp_r.s1 = ramp_r.s2 = 256;
    ramp_g.x0 = 20; ramp_g.y0 = 230; ramp_g.x1 = 300; ramp_g.y1 = 230; ramp_g.x2 = 160; ramp_g.y2 = 10;
    ramp_g.cz0 = 13000; ramp_g.cz1 = 3000; ramp_g.cz2 = 8000;
    ramp_g.col = green; ramp_g.tex = -1; ramp_g.shade = 256; ramp_g.s0 = ramp_g.s1 = ramp_g.s2 = 256;
    raster_flat(&ramp_r);
    raster_flat(&ramp_g);
    CHECK("crossing ramps: left pixel is the nearer red", E.fb[120 * SCR_W + 100] == red);
    CHECK("crossing ramps: right pixel is the nearer green", E.fb[120 * SCR_W + 220] == green);

    clear_sky();
    CHECK("z-buffer resets to far", E.zb[100 * SCR_W + 160] == 0xFFFF);
    a = far_t;
    raster_flat(&a);
    CHECK("far triangle shows again after clear", E.fb[100 * SCR_W + 160] == red);

    build_demo_world();
    world_draw_frame();
    int64_t c1 = fb_checksum();
    int drawn1 = E.tris_drawn;
    build_demo_world();
    world_draw_frame();
    int64_t c2 = fb_checksum();
    CHECK("game scene renders deterministically", c1 == c2 && c1 != 0);
    CHECK("game scene draws real triangles", drawn1 > 50);

    int32_t sx, sy, scz;
    int okp = project_point(W.px, 4096 / 2, W.pz, &sx, &sy, &scz);
    CHECK("hero projects onto the screen", okp && sx > 100 && sx < 220 && sy > 60 && sy < 239);
    int hero_red = 0;
    for (int y = 80; y < 240; y++)
        for (int x = 100; x < 220; x++) {
            uint16_t c = E.fb[y * SCR_W + x];
            if (red_of(c) > 15 && red_of(c) > grn_of(c) + 8 && red_of(c) > blu_of(c) + 8)
                hero_red++;
        }
    CHECK("hero body is hero coloured (z-buffer keeps it whole)", hero_red > 40);

    /* the floor must never tear: stand everywhere, look down */
    int tear_free = 1;
    {
        static const int32_t PZS[7] = {-49152, -32768, -16384, 0, 16384, 32768, 49152};
        static const int32_t PXS[3] = {-16384, 0, 16384};
        for (int zi = 0; zi < 7 && tear_free; zi++) {
            for (int xi = 0; xi < 3 && tear_free; xi++) {
                build_demo_world();
                W.px = PXS[xi];
                W.pz = PZS[zi];
                world_draw_frame();
                int bad = 0;
                for (int y = 225; y < 240; y++)
                    for (int x = 0; x < 320; x++)
                        if (E.fb[y * SCR_W + x] == E.sky_bot) bad++;
                if (bad > 0) tear_free = 0;
            }
        }
    }
    CHECK("the floor never tears open (near-plane clipping)", tear_free);

    long statics = statics_total_bytes();
    long vram = (long)sizeof E.fb + (long)sizeof E.zb + (long)sizeof E.tex;
    printf("PS1 BUDGET framebuffer %ld KB, z-buffer %ld KB, textures %ld KB, video total %ld KB of 1024 KB\n",
           (long)sizeof E.fb / 1024, (long)sizeof E.zb / 1024, (long)sizeof E.tex / 1024, vram / 1024);
    printf("PS1 BUDGET all program memory (static) %ld KB of 2048 KB main RAM\n", statics / 1024);
    CHECK("program fits PS1 main RAM (static budget)", statics < 2048L * 1024);
    CHECK("video fits PS1 VRAM", vram < 1024L * 1024);

    printf("SELFTEST PASS %d FAIL %d\n", pass, fail);
    return fail > 0 ? 1 : 0;
#undef CHECK
}
