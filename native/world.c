/* ============================================================
   world.c - the kindergarten world: same friendly things,
   same rules as easy.alb / the Go program, in fixed point.
   ============================================================ */
#include "psx.h"
#include <string.h>
#include <stdio.h>

World W;

#define U 4096

typedef struct { const char *name; uint16_t col; } NamedColor;
static const NamedColor COLORS[] = {
    {"red", 0}, {"orange", 0}, {"yellow", 0}, {"green", 0}, {"grass", 0},
    {"sky", 0}, {"blue", 0}, {"purple", 0}, {"pink", 0}, {"brown", 0},
    {"white", 0}, {"black", 0}, {"gray", 0}, {"gold", 0}, {"teal", 0}, {"lime", 0},
};
#define NCOLORS 16
static uint16_t color_vals[NCOLORS];
static int colors_ready = 0;

static void init_colors(void) {
    if (colors_ready) return;
    colors_ready = 1;
    color_vals[0] = pack555(31, 5, 5);
    color_vals[1] = pack555(31, 17, 3);
    color_vals[2] = pack555(31, 29, 6);
    color_vals[3] = pack555(7, 24, 8);
    color_vals[4] = pack555(7, 24, 8);
    color_vals[5] = pack555(11, 22, 31);
    color_vals[6] = pack555(6, 9, 30);
    color_vals[7] = pack555(19, 7, 28);
    color_vals[8] = pack555(31, 16, 24);
    color_vals[9] = pack555(17, 10, 4);
    color_vals[10] = pack555(30, 30, 30);
    color_vals[11] = pack555(3, 3, 4);
    color_vals[12] = pack555(18, 18, 19);
    color_vals[13] = pack555(31, 25, 7);
    color_vals[14] = pack555(6, 27, 26);
    color_vals[15] = pack555(18, 31, 8);
}

static int ci_equal(const char *a, const char *b) {
    while (*a && *b) {
        char ca = *a, cb = *b;
        if (ca >= 'A' && ca <= 'Z') ca = (char)(ca - 'A' + 'a');
        if (cb >= 'A' && cb <= 'Z') cb = (char)(cb - 'A' + 'a');
        if (ca != cb) return 0;
        a++; b++;
    }
    return *a == *b;
}

static uint16_t color_of(const char *name) {
    init_colors();
    for (int i = 0; i < NCOLORS; i++)
        if (ci_equal(name, COLORS[i].name)) return color_vals[i];
    return color_vals[8]; /* unknown colour -> pink, never a crash */
}

static int32_t grid(int32_t n) { return n * 2 * U; }

static Item *add_item(Mesh *mesh, int32_t x, int32_t y, int32_t z, int kind, int solid) {
    if (W.item_count >= MAX_ITEMS) return &W.items[0];
    Item *it = &W.items[W.item_count++];
    memset(it, 0, sizeof *it);
    it->th.mesh = mesh;
    it->th.x = x; it->th.y = y; it->th.z = z; it->th.ry = 0;
    it->kind = kind;
    it->solid = solid;
    it->base_y = y;
    return it;
}

void world_init(void) {
    memset(&W, 0, sizeof W);
    strcpy(W.title, "MY GAME");
}

void w_start(const char *title, int quiet) {
    memset(&W, 0, sizeof W);
    strncpy(W.title, title, sizeof W.title - 1);
    pools_reset();
    if (!quiet) {
        printf("==============================\n");
        printf("  %s\n", W.title);
        printf("  A tiny PS1 game. Have fun!\n");
        printf("==============================\n");
    }
}

void w_make_sky(const char *name) {
    uint16_t c = color_of(name);
    engine_set_sky(shade_col(c, 150), shade_col(c, 256));
}

void w_make_ground(const char *name) {
    int32_t tex = -1;
    if (ci_equal(name, "grass") || ci_equal(name, "green")) tex = 0;
    else if (ci_equal(name, "stone")) tex = 1;
    else if (ci_equal(name, "wood")) tex = 3;
    else if (ci_equal(name, "party")) tex = 4;
    else if (ci_equal(name, "sand") || ci_equal(name, "yellow")) tex = 5;
    add_item(mesh_ground(22 * U, tex, color_of(name)), 0, 0, 0, KIND_GROUND, 0);
}

void w_add_tree(int32_t x, int32_t z) {
    uint16_t browns[6] = {pack555(17, 10, 4), pack555(17, 10, 4), pack555(15, 9, 4),
                          pack555(19, 12, 5), pack555(17, 10, 4), pack555(12, 7, 3)};
    add_item(mesh_box(U / 4, U, U / 4, browns), grid(x), U, grid(z), KIND_THING, 1);
    uint16_t greens[5] = {pack555(7, 24, 8), pack555(6, 21, 7), pack555(8, 26, 9),
                          pack555(5, 19, 6), pack555(9, 20, 8)};
    add_item(mesh_pyramid(U + U / 2, U + U / 2, U * 2, greens), grid(x), U * 2, grid(z), KIND_THING, 1);
}

void w_add_house(int32_t x, int32_t z, const char *colname) {
    uint16_t c = color_of(colname), dark = shade_col(c, 170);
    uint16_t cols[6] = {c, dark, dark, c, pack555(24, 9, 6), pack555(10, 8, 8)};
    add_item(mesh_box(U + U / 2, U, U + U / 2, cols), grid(x), U, grid(z), KIND_THING, 1);
    uint16_t reds[5] = {pack555(26, 8, 6), pack555(23, 7, 5), pack555(26, 8, 6),
                        pack555(23, 7, 5), pack555(20, 6, 4)};
    add_item(mesh_pyramid(U * 2, U * 2, U + U / 2, reds), grid(x), U * 2, grid(z), KIND_THING, 1);
}

void w_add_mountain(int32_t x, int32_t z, const char *colname) {
    uint16_t c = color_of(colname);
    uint16_t cols[5] = {c, shade_col(c, 200), c, shade_col(c, 170), shade_col(c, 120)};
    add_item(mesh_pyramid(U * 3, U * 3, U * 5, cols), grid(x), 0, grid(z), KIND_THING, 1);
}

void w_add_box(int32_t x, int32_t z, const char *colname) {
    uint16_t c = color_of(colname);
    uint16_t cols[6] = {c, shade_col(c, 210), shade_col(c, 180), c, shade_col(c, 240), shade_col(c, 130)};
    add_item(mesh_box(U * 3 / 4, U * 3 / 4, U * 3 / 4, cols), grid(x), U * 3 / 4, grid(z), KIND_THING, 1);
}

void w_add_ball(int32_t x, int32_t z, const char *colname) {
    add_item(mesh_gem(U * 3 / 4, U * 3 / 4, color_of(colname)), grid(x), U * 3 / 4, grid(z), KIND_THING, 1);
}

void w_add_gem(int32_t x, int32_t z) {
    add_item(mesh_gem(U * 3 / 4, U, pack555(13, 30, 31)), grid(x), U + U / 3, grid(z), KIND_GEM, 0);
    W.gems_total++;
}

void w_put_player(int32_t x, int32_t z, const char *colname) {
    uint16_t c = color_of(colname);
    W.px = grid(x); W.pz = grid(z); W.dir_sub = 0;
    W.has_player = 1;
    uint16_t front = pack555(30, 30, 30);
    uint16_t cols[6] = {front, shade_col(c, 200), shade_col(c, 180), c, shade_col(c, 230), shade_col(c, 120)};
    Item *p1 = add_item(mesh_box(U / 2 + U / 8, U / 4, U - U / 4, cols), W.px, U / 4, W.pz, KIND_HERO, 0);
    uint16_t win = pack555(11, 22, 31);
    uint16_t cols2[6] = {win, win, win, win, c, shade_col(c, 120)};
    Item *p2 = add_item(mesh_box(U / 3, U / 5, U / 2, cols2), W.px, U / 2 + U / 5, W.pz, KIND_HERO, 0);
    W.hero_idx[0] = (int)(p1 - W.items);
    W.hero_idx[1] = (int)(p2 - W.items);
    W.hero_count = 2;
}

void w_shout(const char *text) {
    strncpy(W.msg, text, sizeof W.msg - 1);
}

/* ---------------- rules ---------------- */
static int blocked_at(int32_t nx, int32_t nz) {
    for (int i = 0; i < W.item_count; i++) {
        Item *it = &W.items[i];
        if (!it->solid) continue;
        int64_t dx = (int64_t)it->th.x - nx, dz = (int64_t)it->th.z - nz;
        if (dx * dx + dz * dz < 37748736) return 1;
    }
    return 0;
}

static void bonk(const char *m) {
    if (W.bonk_cool <= 0) {
        strncpy(W.msg, m, sizeof W.msg - 1);
        W.bonk_cool = 20;
    }
}

static int try_move(int32_t dx, int32_t dz) {
    int32_t nx = W.px + dx, nz = W.pz + dz;
    if (nx > 19 * U || nx < -19 * U || nz > 19 * U || nz < -19 * U) {
        bonk("BONK! THE EDGE!");
        return 0;
    }
    if (blocked_at(nx, nz)) {
        bonk("BONK!");
        return 0;
    }
    W.px = nx; W.pz = nz;
    return 1;
}

static void check_gems(void) {
    for (int i = 0; i < W.item_count; i++) {
        Item *it = &W.items[i];
        if (it->kind == KIND_GEM && !it->got) {
            int64_t dx = (int64_t)it->th.x - W.px, dz = (int64_t)it->th.z - W.pz;
            if (dx * dx + dz * dz < 42991616) {
                it->got = 1;
                W.gems_got++;
                strncpy(W.msg, "YOU GOT A GEM!", sizeof W.msg - 1);
                spawn_sparkle(it->th.x, it->th.y, it->th.z, pack555(31, 29, 6), 14);
                printf("%d\n", W.gems_got); /* shfaq: reveal the count */
                fflush(stdout);
            }
        }
    }
    if (W.gems_total > 0 && W.gems_got >= W.gems_total) W.won = 1;
}

int32_t world_dir_brads(void) {
    int32_t d = (W.dir_sub + 512) / 1024;
    d %= 256;
    if (d < 0) d += 256;
    return d;
}

void world_update(int dt_ms, int fwd, int back, int left, int right) {
    if (W.bonk_cool > 0) W.bonk_cool--;
    if (W.won) return;
    const int32_t turn_per_ms = 100 * 1024 / 1000; /* 100 brads/sec */
    if (left) W.dir_sub -= turn_per_ms * dt_ms;
    if (right) W.dir_sub += turn_per_ms * dt_ms;
    int mf = (fwd ? 1 : 0) - (back ? 1 : 0);
    if (mf != 0) {
        int32_t dir = world_dir_brads();
        int32_t dist = 7 * U * dt_ms / 1000;
        int32_t dx = sin_a(dir) * mf * dist / FIXED;
        int32_t dz = cos_a(dir) * mf * dist / FIXED;
        try_move(dx, dz);
    }
    check_gems();
}

void world_bot_greedy(void) {
    if (W.won || W.gems_total == 0) return;
    Item *best = NULL;
    int64_t bd = -1;
    for (int i = 0; i < W.item_count; i++) {
        Item *it = &W.items[i];
        if (it->kind == KIND_GEM && !it->got) {
            int64_t dx = (int64_t)it->th.x - W.px, dz = (int64_t)it->th.z - W.pz;
            int64_t d2 = dx * dx + dz * dz;
            if (bd < 0 || d2 < bd) { bd = d2; best = it; }
        }
    }
    if (!best) return;
    if (W.avoid > 0) {
        W.dir_sub += 4 * 1024;
        W.avoid--;
    } else {
        int32_t want = atan2_brads(best->th.x - W.px, best->th.z - W.pz);
        int32_t diff = want - W.dir_sub;
        const int32_t full = 256 * 1024;
        while (diff > full / 2) diff -= full;
        while (diff < -full / 2) diff += full;
        int32_t step = 100 * 1024 / 30;
        if (diff > step) W.dir_sub += step;
        else if (diff < -step) W.dir_sub -= step;
        else W.dir_sub = want;
    }
    int32_t dir = world_dir_brads();
    int32_t step = 7 * U / 30;
    int32_t mx = sin_a(dir) * step / FIXED;
    int32_t mz = cos_a(dir) * step / FIXED;
    if (!try_move(mx, mz)) W.avoid = 30;
    check_gems();
}

void world_discrete_step(int ch) {
    int32_t dir = world_dir_brads();
    switch (ch) {
        case 'w': try_move(sin_a(dir), cos_a(dir)); break;
        case 's': try_move(-sin_a(dir), -cos_a(dir)); break;
        case 'a': W.dir_sub -= 16 * 1024; break;
        case 'd': W.dir_sub += 16 * 1024; break;
    }
    check_gems();
}

static Thing frame_things[MAX_ITEMS];

void world_draw_frame(void) {
    W.frame++;
    int32_t dir = world_dir_brads();
    int n = 0;
    for (int i = 0; i < W.item_count; i++) {
        Item *it = &W.items[i];
        if (it->kind == KIND_GEM) {
            it->th.ry = (W.frame * 2) % 256;
            it->th.y = it->base_y + (sin_a((W.frame * 2) % 256) * (U / 5)) / U;
        }
        if (it->kind == KIND_HERO) {
            it->th.x = W.px;
            it->th.z = W.pz;
            it->th.ry = (256 - dir) % 256;
        }
        if (!it->got) frame_things[n++] = it->th;
    }
    int32_t cam_x = W.px - sin_a(dir) * 8;
    int32_t cam_z = W.pz - cos_a(dir) * 8;
    engine_set_camera(cam_x, U * 4, cam_z, dir, -14);
    engine_render(frame_things, n);
    char hud[32];
    snprintf(hud, sizeof hud, "GEMS %d/%d", W.gems_got, W.gems_total);
    draw_text(8, 8, hud, pack555(31, 31, 31), 2);
    if (W.msg[0]) draw_text(8, 218, W.msg, pack555(31, 29, 6), 2);
    if (W.won) {
        fill_rect(58, 88, 240, 142, pack555(2, 2, 6));
        draw_text(76, 96, "YOU WIN", pack555(31, 29, 6), 4);
        char sc[32];
        snprintf(sc, sizeof sc, "SCORE %d", W.gems_got * 100);
        draw_text(88, 124, sc, pack555(31, 31, 31), 2);
    }
    step_parts();
}
