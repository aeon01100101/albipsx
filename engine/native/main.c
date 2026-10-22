/* ============================================================
   main.c - AlbiPSX, the Linux program that fits a PS1.

     albipsx game.alb                  window play
     albipsx game.alb --headless       the robot plays
     albipsx game.alb --shot out.png   save a picture
     albipsx --selftest                prove the z-buffer + RAM

   The game file is written in the friendly easy.* words.
   This program reads those words with a tiny built-in
   reader (the whole Albish language would never fit in a
   PS1 - the friendly words are the game language here).
   Static binary, static memory: its peak RAM is printed
   at the end of every run, next to the PS1's 2048 KB.
   ============================================================ */
#include "psx.h"
#include "x11.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <time.h>
#include <sys/resource.h>

#define NATIVE_VERSION "1.2.0"

typedef struct {
    int headless, force_window, frames, scale, xcheck;
    const char *shot;
    int played, frames_rendered;
} Ctl;

static Ctl CTL;
static char game_buf[65536];

/* ---------------- RAM honesty ---------------- */
long self_peak_rss_kb(void) {
    /* Read our own VmHWM (the true high-water RSS of THIS
       process). getrusage is NOT used: its maxrss counter is
       inherited across fork(), so a big parent (a shell chain,
       a Python launcher) inflates it with memory this process
       never touched. VmHWM measures only this process. */
    FILE *f = fopen("/proc/self/status", "r");
    if (f) {
        char ln[160];
        long hwm = -1;
        while (fgets(ln, sizeof ln, f)) {
            if (strncmp(ln, "VmHWM:", 6) == 0) {
                hwm = strtol(ln + 6, NULL, 10);
                break;
            }
        }
        fclose(f);
        if (hwm > 0) return hwm;
    }
    struct rusage ru;
    if (getrusage(RUSAGE_SELF, &ru) != 0) return -1;
    return (long)ru.ru_maxrss;
}

long statics_total_bytes(void); /* in render.c */

void print_ram_report(const char *when) {
    long statics = statics_total_bytes() + x_static_bytes() + (long)sizeof game_buf;
    long vram = (long)sizeof E.fb + (long)sizeof E.zb + (long)sizeof E.tex;
    long peak = self_peak_rss_kb();
    printf("PS1 BUDGET (%s) program memory %ld KB of 2048 KB main | video %ld KB of 1024 KB\n",
           when, statics / 1024, vram / 1024);
    if (peak >= 0)
        printf("RAM peak RSS %ld KB (PS1 main RAM 2048 KB) -> %s\n",
               peak, peak <= 2048 ? "WITHIN PS1 LIMITS" : "OVER PS1 LIMITS");
}

/* ---------------- shots + summaries ---------------- */
static void save_shot(void) {
    if (!CTL.shot || !CTL.shot[0]) return;
    const char *dot = strrchr(CTL.shot, '.');
    int rc;
    if (dot && strcmp(dot, ".ppm") == 0) rc = save_ppm(CTL.shot);
    else rc = save_png(CTL.shot);
    if (rc == 0) printf("saved %s\n", CTL.shot);
    else fprintf(stderr, "could not save %s\n", CTL.shot);
}

static void summary(const char *how) {
    printf("SESSION frames %d gems %d of %d won %s (%s)\n",
           CTL.frames_rendered, W.gems_got, W.gems_total,
           W.won ? "true" : "false", how);
}

/* ---------------- sessions ---------------- */
static void run_headless_bot(void) {
    int frames = CTL.frames > 0 ? CTL.frames : 1800;
    int extra = 0;
    for (int f = 0; f < frames; f++) {
        world_bot_greedy();
        world_draw_frame();
        CTL.frames_rendered++;
        if (W.won && ++extra > 20) break;
    }
    save_shot();
    summary("headless robot");
    print_ram_report("headless");
}

static void run_window_session(void); /* below */

static void run_session(void) {
    if (CTL.headless) {
        run_headless_bot();
        return;
    }
    run_window_session();
}

static char bot_moves[64][128];
static int bot_move_count = 0;

static void run_bot_session(void) {
    /* window present-if-possible, like the Go program */
    XConn xc;
    int have_win = 0;
    uint32_t win = 0, gc = 0;
    char wtitle[96];
    snprintf(wtitle, sizeof wtitle, "AlbiPSX - %s", W.title);
    if (!CTL.headless && x_open(&xc) == 0) {
        if (x_create_window(&xc, SCR_W * CTL.scale, SCR_H * CTL.scale, wtitle, &win, &gc) == 0)
            have_win = 1;
    }
    world_draw_frame();
    CTL.frames_rendered++;
    if (have_win) {
        x_put_image(&xc, win, gc, E.fb, CTL.scale);
        struct timespec ts = {0, 200 * 1000 * 1000};
        nanosleep(&ts, NULL);
    }
    for (int li = 0; li < bot_move_count; li++) {
        printf("The robot player types: %s\n", bot_moves[li]);
        fflush(stdout);
        for (const char *p = bot_moves[li]; *p; p++)
            world_discrete_step((unsigned char)*p);
        world_draw_frame();
        CTL.frames_rendered++;
        if (have_win) {
            x_put_image(&xc, win, gc, E.fb, CTL.scale);
            struct timespec ts = {0, 350 * 1000 * 1000};
            nanosleep(&ts, NULL);
        }
    }
    if (W.won) {
        printf("********************************\n");
        printf("  YOU WIN! YOU FOUND ALL THE GEMS!\n");
        printf("  Score: %d\n", W.gems_got * 100);
        printf("********************************\n");
    }
    printf("BOT DONE GEMS %d OF %d\n", W.gems_got, W.gems_total);
    save_shot();
    if (have_win) x_close(&xc);
    print_ram_report("play_bot");
}

/* ---------------- window play ---------------- */
static long now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000L + ts.tv_nsec / 1000000L;
}

static int shift_for_mask(uint32_t mask) {
    int s = 0;
    while (mask && !(mask & 1)) { mask >>= 1; s++; }
    return s;
}

static void xcheck_window(XConn *xc, uint32_t win, int win_w, int win_h) {
    int px[192], py[192];
    uint32_t vals[192];
    int n = 0;
    for (int gy = 0; gy < 12; gy++)
        for (int gx = 0; gx < 16; gx++) {
            px[n] = gx * (win_w - 8) / 16 + 4;
            py[n] = gy * (win_h - 8) / 12 + 4;
            n++;
        }
    uint32_t rm, gm, bm;
    int lsb;
    if (x_get_image_points(xc, win, win_w, win_h, px, py, n, vals, &rm, &gm, &bm, &lsb) != 0) {
        printf("XCHECK FAIL no image came back\n");
        return;
    }
    int rsh = shift_for_mask(rm), gsh = shift_for_mask(gm), bsh = shift_for_mask(bm);
    int bad = 0;
    for (int i = 0; i < n; i++) {
        uint32_t v = vals[i];
        long r8 = (v >> rsh) & 0xff, g8 = (v >> gsh) & 0xff, b8 = (v >> bsh) & 0xff;
        int fx = px[i] * SCR_W / win_w, fy = py[i] * SCR_H / win_h;
        uint16_t c = E.fb[fy * SCR_W + fx];
        long er = red_of(c) * 255 / 31, eg = grn_of(c) * 255 / 31, eb = blu_of(c) * 255 / 31;
        long diff = labs(r8 - er) + labs(g8 - eg) + labs(b8 - eb);
        if (diff > 24) bad++;
    }
    if (bad == 0) printf("XCHECK PASS window pixels match the framebuffer (%d points)\n", n);
    else printf("XCHECK FAIL %d of %d points differ\n", bad, n);
    fflush(stdout);
}

static void run_window_session(void) {
    XConn xc;
    if (x_open(&xc) != 0) {
        fprintf(stderr, "window: %s\n", xc.err);
        fprintf(stderr, "falling back to the headless robot\n");
        run_headless_bot();
        return;
    }
    int scale = CTL.scale;
    if (scale < 1) scale = 1;
    if (scale > 4) scale = 4;
    int win_w = SCR_W * scale, win_h = SCR_H * scale;
    uint32_t win, gc;
    char wtitle[96];
    snprintf(wtitle, sizeof wtitle, "AlbiPSX - %s", W.title);
    x_create_window(&xc, win_w, win_h, wtitle, &win, &gc);
    uint32_t protocols = 0, del_atom = 0;
    x_set_wm_delete(&xc, win, &protocols, &del_atom);
    (void)protocols;
    /* bindings: evdev defaults first, server map overrides */
    /* actions: 0 none, 1 fwd, 2 back, 3 left, 4 right, 5 quit */
    uint8_t action[256];
    memset(action, 0, sizeof action);
    action[25] = 1; action[38] = 3; action[39] = 2; action[40] = 4; action[24] = 5;
    action[9] = 5; action[111] = 1; action[113] = 3; action[116] = 2; action[114] = 4;
    uint8_t keys[10];
    x_keyboard_map(&xc, keys);
    static const int ACT[10] = {1, 3, 2, 4, 5, 5, 1, 3, 2, 4}; /* w a s d q esc up left down right */
    for (int i = 0; i < 10; i++)
        if (keys[i]) action[keys[i]] = (uint8_t)ACT[i];
    printf("Window open: w a s d or arrow keys to drive, q to stop.\n");
    fflush(stdout);
    int state[6] = {0, 0, 0, 0, 0, 0};
    int quit = 0, frames = 0, xchecked = 0;
    long last = now_ms();
    while (!quit) {
        uint8_t ev[32];
        while (x_poll_event(&xc, ev)) {
            int code = ev[0] & 0x7f;
            if (code == 2) {
                int a = action[ev[1]];
                if (a == 5) quit = 1;
                else if (a) state[a] = 1;
            } else if (code == 3) {
                int a = action[ev[1]];
                if (a) state[a] = 0;
            } else if (code == 33 && del_atom) {
                uint32_t v = (uint32_t)ev[12] | ((uint32_t)ev[13] << 8) |
                             ((uint32_t)ev[14] << 16) | ((uint32_t)ev[15] << 24);
                if (v == del_atom) quit = 1;
            }
        }
        long now = now_ms();
        long dt = now - last;
        last = now;
        if (dt > 50) dt = 50;
        if (dt <= 0) dt = 1;
        world_update((int)dt, state[1], state[2], state[3], state[4]);
        world_draw_frame();
        CTL.frames_rendered++;
        frames++;
        x_put_image(&xc, win, gc, E.fb, scale);
        if (CTL.xcheck && !xchecked && frames >= 5) {
            xchecked = 1;
            xcheck_window(&xc, win, win_w, win_h);
        }
        if (CTL.frames > 0 && frames >= CTL.frames) break;
        long spent = now_ms() - now;
        if (spent < 33) {
            struct timespec ts = {0, (33 - spent) * 1000 * 1000};
            nanosleep(&ts, NULL);
        }
    }
    if (xc.err[0]) fprintf(stderr, "X reported: %s\n", xc.err);
    x_close(&xc);
    save_shot();
    summary("window");
    print_ram_report("window");
}

/* ============================================================
   The friendly-words reader. It understands exactly the
   kindergarten vocabulary - one easy.* call per line -
   and nothing else. Anything fancier belongs to the big
   Albish interpreter, which could never fit in a PS1.
   ============================================================ */
typedef struct {
    int is_str;
    char str[128];
    long num;
    int is_list;
    char list[64][128];
    int list_count;
} Arg;

static char *trim(char *s) {
    while (*s == ' ' || *s == '\t' || *s == '\r') s++;
    char *e = s + strlen(s);
    while (e > s && (e[-1] == ' ' || e[-1] == '\t' || e[-1] == '\r')) *--e = 0;
    return s;
}

static void strip_comment(char *s) {
    int in_str = 0;
    for (; *s; s++) {
        if (*s == '"') in_str = !in_str;
        else if (*s == '#' && !in_str) { *s = 0; return; }
    }
}

static int parse_arg(char *s, Arg *a) {
    memset(a, 0, sizeof *a);
    s = trim(s);
    if (s[0] == '"') {
        a->is_str = 1;
        char *e = strchr(s + 1, '"');
        if (!e) return -1;
        size_t n = (size_t)(e - (s + 1));
        if (n >= sizeof a->str) n = sizeof a->str - 1;
        for (size_t i = 0; i < n; i++) a->str[i] = s[1 + i];
        a->str[n] = 0;
        return 0;
    }
    if (s[0] == '[') {
        a->is_list = 1;
        char *p = s;
        while ((p = strchr(p, '"')) != NULL && a->list_count < 64) {
            char *e = strchr(p + 1, '"');
            if (!e) break;
            size_t n = (size_t)(e - (p + 1));
            if (n >= 128) n = 127;
            for (size_t i = 0; i < n; i++) a->list[a->list_count][i] = p[1 + i];
            a->list[a->list_count][n] = 0;
            a->list_count++;
            p = e + 1;
        }
        return 0;
    }
    a->num = strtol(s, NULL, 10);
    return 0;
}

static int split_args(char *inside, Arg *args, int maxargs) {
    int n = 0, depth = 0, in_str = 0;
    char *start = inside;
    for (char *p = inside;; p++) {
        char c = *p;
        if (c == '"') in_str = !in_str;
        if (!in_str) {
            if (c == '[') depth++;
            if (c == ']') depth--;
            if ((c == ',' && depth == 0) || c == 0) {
                char save = *p;
                *p = 0;
                if (n < maxargs && parse_arg(start, &args[n]) == 0) n++;
                *p = save;
                start = p + 1;
                if (c == 0) break;
            }
        }
        if (c == 0) break;
    }
    return n;
}

static int game_error(int lineno, const char *line, const char *what) {
    fprintf(stderr, "line %d: %s\n  %s\n", lineno, what, line);
    fprintf(stderr, "The PS1 program only speaks the friendly easy.* words.\n");
    return 1;
}

static int run_game_text(char *text, const char *path) {
    (void)path;
    int lineno = 0;
    char *cursor = text;
    while (*cursor) {
        char *eol = strchr(cursor, '\n');
        if (eol) *eol = 0;
        lineno++;
        char *line = cursor;
        cursor = eol ? eol + 1 : cursor + strlen(cursor);
        strip_comment(line);
        char *t = trim(line);
        if (!t[0]) continue;
        if (strncmp(t, "import", 6) == 0) continue;
        if (strncmp(t, "easy.", 5) != 0)
            return game_error(lineno, t, "I don't know this line.");
        char *open = strchr(t, '(');
        if (!open) return game_error(lineno, t, "Missing ( )");
        char name[32];
        size_t nn = (size_t)(open - (t + 5));
        if (nn >= sizeof name) return game_error(lineno, t, "Word too long.");
        memcpy(name, t + 5, nn);
        name[nn] = 0;
        char *close = strrchr(t, ')');
        if (!close) return game_error(lineno, t, "Missing )");
        *close = 0;
        static Arg args[6];
        int na = split_args(open + 1, args, 6);
        const char *s0 = na > 0 && args[0].is_str ? args[0].str : "";
        const char *s2 = na > 2 && args[2].is_str ? args[2].str : "";
        long n0 = na > 0 ? args[0].num : 0;
        long n1 = na > 1 ? args[1].num : 0;
        if (strcmp(name, "start") == 0) w_start(s0, 0);
        else if (strcmp(name, "start_quiet") == 0) w_start(s0, 1);
        else if (strcmp(name, "make_sky") == 0) w_make_sky(s0);
        else if (strcmp(name, "make_ground") == 0) w_make_ground(s0);
        else if (strcmp(name, "add_tree") == 0) w_add_tree((int32_t)n0, (int32_t)n1);
        else if (strcmp(name, "add_house") == 0) w_add_house((int32_t)n0, (int32_t)n1, s2);
        else if (strcmp(name, "add_mountain") == 0) w_add_mountain((int32_t)n0, (int32_t)n1, s2);
        else if (strcmp(name, "add_box") == 0) w_add_box((int32_t)n0, (int32_t)n1, s2);
        else if (strcmp(name, "add_ball") == 0) w_add_ball((int32_t)n0, (int32_t)n1, s2);
        else if (strcmp(name, "add_gem") == 0) w_add_gem((int32_t)n0, (int32_t)n1);
        else if (strcmp(name, "put_player") == 0) w_put_player((int32_t)n0, (int32_t)n1, s2);
        else if (strcmp(name, "shout") == 0) w_shout(s0);
        else if (strcmp(name, "play") == 0) {
            CTL.played = 1;
            run_session();
        } else if (strcmp(name, "play_bot") == 0) {
            CTL.played = 1;
            bot_move_count = 0;
            if (na > 0 && args[0].is_list) {
                for (int i = 0; i < args[0].list_count && bot_move_count < 64; i++) {
                    strncpy(bot_moves[bot_move_count], args[0].list[i], 127);
                    bot_moves[bot_move_count][127] = 0;
                    bot_move_count++;
                }
            }
            run_bot_session();
        } else {
            return game_error(lineno, t, "I don't know this friendly word.");
        }
    }
    return 0;
}

/* ---------------- flags + main ---------------- */
static int is_value_flag(const char *a) {
    return strcmp(a, "--frames") == 0 || strcmp(a, "--shot") == 0 || strcmp(a, "--scale") == 0;
}

int run_selftest(void); /* in render.c */

int main(int argc, char **argv) {
    /* reorder: flags may come before or after the game file */
    static char *flags[64], *pos[64];
    int nf = 0, np = 0;
    for (int i = 1; i < argc; i++) {
        char *a = argv[i];
        if (a[0] == '-' && a[1]) {
            if (nf < 62) flags[nf++] = a;
            if (is_value_flag(a) && i + 1 < argc && nf < 63) flags[nf++] = argv[++i];
        } else {
            if (np < 63) pos[np++] = a;
        }
    }
    memset(&CTL, 0, sizeof CTL);
    CTL.scale = 3;
    int selftest = 0, version = 0;
    const char *game = "games/game.alb";
    for (int i = 0; i < nf; i++) {
        char *a = flags[i];
        if (strcmp(a, "--headless") == 0) CTL.headless = 1;
        else if (strcmp(a, "--window") == 0) CTL.force_window = 1;
        else if (strcmp(a, "--xcheck") == 0) CTL.xcheck = 1;
        else if (strcmp(a, "--selftest") == 0) selftest = 1;
        else if (strcmp(a, "--version") == 0) version = 1;
        else if (strcmp(a, "--frames") == 0 && i + 1 < nf) CTL.frames = atoi(flags[++i]);
        else if (strcmp(a, "--scale") == 0 && i + 1 < nf) CTL.scale = atoi(flags[++i]);
        else if (strcmp(a, "--shot") == 0 && i + 1 < nf) CTL.shot = flags[++i];
        else if (strchr(a, '=') != NULL) {
            /* --name=value form */
            char *eq = strchr(a, '=');
            if (strncmp(a, "--frames=", 9) == 0) CTL.frames = atoi(eq + 1);
            else if (strncmp(a, "--scale=", 8) == 0) CTL.scale = atoi(eq + 1);
            else if (strncmp(a, "--shot=", 7) == 0) CTL.shot = eq + 1;
        }
    }
    if (np > 0) game = pos[0];
    if (version) {
        printf("AlbiPSX %s (the PS1-sized Linux program)\n", NATIVE_VERSION);
        return 0;
    }
    engine_init();
    world_init();
    if (selftest) return run_selftest();
    FILE *f = fopen(game, "rb");
    if (!f) {
        fprintf(stderr, "cannot read game file: %s\n", game);
        return 1;
    }
    size_t len = fread(game_buf, 1, sizeof game_buf - 1, f);
    fclose(f);
    game_buf[len] = 0;
    if (!CTL.force_window && !CTL.headless && !getenv("DISPLAY")) {
        CTL.headless = 1;
        printf("No screen found (DISPLAY is empty) - running headless.\n");
    }
    int rc = run_game_text(game_buf, game);
    if (rc != 0) return rc;
    if (!CTL.played && CTL.shot && W.item_count > 0) {
        world_draw_frame();
        CTL.frames_rendered++;
        save_shot();
    }
    return 0;
}
