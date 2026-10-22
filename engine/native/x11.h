/* x11.h - the tiny X11 client (pure C, sockets + syscalls,
   no Xlib). Just enough protocol to open a window, paint
   frames into it, and read the keyboard. */
#ifndef X11_H
#define X11_H

#include <stdint.h>

typedef struct {
    int fd;
    uint32_t root, root_visual, black_pixel;
    uint32_t red_mask, green_mask, blue_mask;
    int max_req_len;       /* in 4-byte units */
    int min_key, max_key;
    uint16_t seq;
    char err[128];
    /* event stash: packets that arrive while waiting for a reply */
    uint8_t evq[64][32];
    int evq_head, evq_tail;
} XConn;

int x_open(XConn *x);                       /* 0 ok, -1 fail (x->err set) */
void x_close(XConn *x);
int x_create_window(XConn *x, int w, int h, const char *title, uint32_t *win, uint32_t *gc);
uint32_t x_intern_atom(XConn *x, const char *name);
void x_set_wm_delete(XConn *x, uint32_t win, uint32_t *protocols, uint32_t *del_atom);
/* fills keys[10] with keycodes for: w a s d q esc up left down right.
   Best effort: leaves 0 for any the server won't tell us. */
void x_keyboard_map(XConn *x, uint8_t keys[10]);
void x_put_image(XConn *x, uint32_t win, uint32_t gc, const uint16_t *fb, int scale);
int x_poll_event(XConn *x, uint8_t ev[32]);  /* 1 = event returned */
/* reads back npts window pixels at (px,py) into outvals (raw u32s) */
int x_get_image_points(XConn *x, uint32_t win, int w, int h,
                       const int *px, const int *py, int npts, uint32_t *outvals,
                       uint32_t *red_mask, uint32_t *green_mask, uint32_t *blue_mask, int *img_lsb);
long x_static_bytes(void);

#endif
