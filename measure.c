/* measure.c - run a command and report its TRUE peak RSS.
   This tiny launcher exists because fork() inherits the
   parent's rusage high-water mark: a big parent (Python, a
   shell chain) makes every child LOOK huge. This parent is
   a few pages big, and wait4() then reports the child's own
   peak honestly. Build: gcc -O2 -static -o measure measure.c */
#include <sys/types.h>
#include <sys/wait.h>
#include <sys/resource.h>
#include <unistd.h>
#include <stdio.h>
#include <time.h>

int main(int argc, char **argv) {
    if (argc < 2) {
        fprintf(stderr, "usage: measure <command...>\n");
        return 2;
    }
    struct timespec t0, t1;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    pid_t pid = fork();
    if (pid == 0) {
        execvp(argv[1], &argv[1]);
        perror("exec");
        _exit(127);
    }
    int status = 0;
    struct rusage ru;
    wait4(pid, &status, 0, &ru);
    clock_gettime(CLOCK_MONOTONIC, &t1);
    double elapsed = (double)(t1.tv_sec - t0.tv_sec) + (double)(t1.tv_nsec - t0.tv_nsec) / 1e9;
    int code = WIFEXITED(status) ? WEXITSTATUS(status) : 1;
    printf("RAM: peak %.1f MB | time %.2fs | exit %d\n",
           ru.ru_maxrss / 1024.0, elapsed, code);
    return code;
}
