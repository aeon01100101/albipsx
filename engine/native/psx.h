/* ============================================================
   psx.h - shared heart of the AlbiPSX Linux program.
   Everything is static: the whole engine lives in fixed
   arrays sized at build time, so its RAM use is known to
   the byte before it ever runs. PS1 rules:
     main RAM 2048 KB - the whole program must fit
     video RAM 1024 KB - picture + depths + textures must fit
   ============================================================ */
#ifndef PSX_H
#define PSX_H

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>

#define SCR_W 320
#define SCR_H 240
#define SCR_PIX 76800
#define FIXED 4096
#define FOCAL 230
#define NEAR_Z 1200
#define SNAP_Q 24
#define MAX_TRIS 600
#define MAX_PARTS 64
#define MAX_ITEMS 256
#define MAX_MESHES 64
#define VPOOL 2048
#define FPOOL 2048
#define UPOOL 2048
#define TEX_COUNT 6
#define TEX_SHADE_CACHE 8

static inline int32_t clampi(int32_t v, int32_t lo, int32_t hi) {
    return v < lo ? lo : (v > hi ? hi : v);
}
static inline int32_t mini32(int32_t a, int32_t b) { return a < b ? a : b; }
static inline int32_t maxi32(int32_t a, int32_t b) { return a > b ? a : b; }
static inline int32_t fmul(int32_t a, int32_t b) {
    return (int32_t)(((int64_t)a * (int64_t)b) / FIXED);
}

/* ---- colours, 15-bit RGB555 packed in a uint16 ---- */
uint16_t pack555(int32_t r, int32_t g, int32_t b);
int32_t red_of(uint16_t c);
int32_t grn_of(uint16_t c);
int32_t blu_of(uint16_t c);
uint16_t shade_col(uint16_t c, int32_t s);
uint16_t mix_col(uint16_t c1, uint16_t c2, int32_t t);

/* ---- meshes (built into static pools) ---- */
typedef struct {
    int32_t vstart, vcount;   /* into vert_pool / uv_pool */
    int32_t fstart, fcount;   /* into face_pool */
    int32_t vsh[8];
    int has_vsh;
    int ground;
} Mesh;

extern int32_t vert_pool[VPOOL][3];
extern int32_t uv_pool[UPOOL][2];
extern int32_t face_pool[FPOOL][5]; /* a b c colour tex */
extern Mesh meshes[MAX_MESHES];
extern int mesh_count;

void pools_reset(void);
Mesh *mesh_new(void);
int mesh_add_vert(Mesh *m, int32_t x, int32_t y, int32_t z, int32_t u, int32_t v);
void mesh_add_face(Mesh *m, int32_t a, int32_t b, int32_t c, uint16_t col, int32_t tex);

Mesh *mesh_box(int32_t hx, int32_t hy, int32_t hz, const uint16_t cols[6]);
Mesh *mesh_pyramid(int32_t hx, int32_t hz, int32_t hgt, const uint16_t cols[5]);
Mesh *mesh_gem(int32_t r, int32_t hgt, uint16_t col);
Mesh *mesh_ground(int32_t half, int32_t texid, uint16_t col);

/* ---- the engine ---- */
typedef struct {
    int32_t x, y, z, ry;
    Mesh *mesh;
} Thing;

typedef struct {
    uint16_t fb[SCR_PIX];        /* the picture:      150 KB of VRAM */
    uint16_t zb[SCR_PIX];        /* depths (z >> 3):  150 KB of VRAM */
    uint16_t tex[TEX_COUNT][1024];
    uint16_t sky_top, sky_bot;
    int32_t cam_x, cam_y, cam_z, cam_cos, cam_sin, pit_cos, pit_sin;
    int32_t lx, ly, lz;
    int tick;
    int tris_seen, tris_drawn, pixels;
    uint32_t rng;
} Engine;

extern Engine E;

void engine_init(void);
int32_t sin_a(int32_t a);
int32_t cos_a(int32_t a);
void engine_set_sky(uint16_t top, uint16_t bot);
void engine_set_camera(int32_t x, int32_t y, int32_t z, int32_t yaw, int32_t pitch);
int engine_render(Thing *things, int nthings);
int project_point(int32_t wx, int32_t wy, int32_t wz, int32_t *sx, int32_t *sy, int32_t *cz);
void fill_rect(int32_t x0, int32_t y0, int32_t x1, int32_t y1, uint16_t col);
void draw_text(int32_t x, int32_t y, const char *text, uint16_t col, int32_t scale);
int64_t fb_checksum(void);
int save_png(const char *path);
int save_ppm(const char *path);

/* sparkles */
void spawn_sparkle(int32_t x, int32_t y, int32_t z, uint16_t col, int n);
void step_parts(void);
int parts_alive(void);

/* fixed-point atan2 -> angle in 1/1024 brads, for the robot */
int32_t atan2_brads(int32_t y, int32_t x);

/* ---- the world (the kindergarten game) ---- */
enum { KIND_THING = 0, KIND_GEM = 1, KIND_HERO = 2, KIND_GROUND = 3 };

typedef struct {
    Thing th;
    int kind;
    int solid;
    int got;
    int32_t base_y;
} Item;

typedef struct {
    char title[64];
    Item items[MAX_ITEMS];
    int item_count;
    int hero_idx[2];
    int hero_count;
    int gems_total, gems_got;
    int32_t px, pz;
    int32_t dir_sub;      /* facing, in 1/1024 brads */
    char msg[64];
    int frame;
    int won;
    int has_player;
    int bonk_cool;
    int avoid;
} World;

extern World W;

void world_init(void);
void w_start(const char *title, int quiet);
void w_make_sky(const char *name);
void w_make_ground(const char *name);
void w_add_tree(int32_t x, int32_t z);
void w_add_house(int32_t x, int32_t z, const char *col);
void w_add_mountain(int32_t x, int32_t z, const char *col);
void w_add_box(int32_t x, int32_t z, const char *col);
void w_add_ball(int32_t x, int32_t z, const char *col);
void w_add_gem(int32_t x, int32_t z);
void w_put_player(int32_t x, int32_t z, const char *col);
void w_shout(const char *text);
void world_draw_frame(void);
void world_update(int dt_ms, int fwd, int back, int left, int right);
void world_bot_greedy(void);
void world_discrete_step(int ch);
int32_t world_dir_brads(void);

/* RAM honesty: engine static budget + the process's own peak */
long render_static_bytes(void);
long self_peak_rss_kb(void);
void print_ram_report(const char *when);

#endif
