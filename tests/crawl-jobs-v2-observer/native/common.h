#ifndef OBS1_COMMON_H
#define OBS1_COMMON_H
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <stdatomic.h>
#include <stdint.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>
#include "cases.h"

#define OBS_MAGIC UINT64_C(0x4f42533143303031)
#define OBS_EFFECTS 1050
#define OBS_EVENTS 8192
#define OBS_READ_BYTES (4u * 1024u * 1024u)
#define OBS_ACTIVE_NS UINT64_C(2000000000)
#define OBS_HOLD_NS UINT64_C(20000000)

struct witness {
    uint64_t magic;
    _Atomic uint64_t completed;
    _Atomic uint64_t ordinal;
    _Atomic uint64_t prototype;
    _Atomic uint64_t bytecode_pc;
    _Atomic uint64_t pre_ns;
    _Atomic uint64_t post_ns;
    _Atomic uint64_t heartbeat;
    _Atomic uint64_t finished;
    _Atomic uint64_t last_value;
    uint64_t transcript[OBS_EFFECTS];
};
_Static_assert(sizeof(struct witness) == 8480, "closed witness ABI");

static inline uint64_t mono_ns(void) {
    struct timespec now;
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) _exit(70);
    return (uint64_t)now.tv_sec * UINT64_C(1000000000) + (uint64_t)now.tv_nsec;
}

static inline void invalid(const char *reason) {
    /* reason is always a source literal, never an errno/path/register/request. */
    char output[192];
    int length = snprintf(output, sizeof output,
        "{\"cleanup_required\":true,\"reason\":\"%s\",\"status\":\"INVALID\",\"version\":1}\n", reason);
    if (length > 0 && (size_t)length < sizeof output) {
        int flags = fcntl(STDOUT_FILENO, F_GETFL);
        if (flags < 0 || fcntl(STDOUT_FILENO, F_SETFL, flags | O_NONBLOCK) < 0) _exit(1);
        if (write(STDOUT_FILENO, output, (size_t)length) != length) _exit(1);
    }
    _exit(1);
}

static inline void caps_zero(void) {
    if (getuid() != 999 || geteuid() != 999 || getgid() != 999 || getegid() != 999 ||
        prctl(PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0) != 1) invalid("admission");
    FILE *stream = fopen("/proc/self/status", "re");
    if (!stream) invalid("admission");
    char line[512];
    unsigned found = 0;
    const char *names[] = {"CapInh:", "CapPrm:", "CapEff:", "CapBnd:", "CapAmb:"};
    while (fgets(line, sizeof line, stream)) {
        for (unsigned i = 0; i < 5; i++) {
            if (strncmp(line, names[i], strlen(names[i])) != 0) continue;
            unsigned long long value;
            char extra;
            if (found & (1u << i) || sscanf(line + strlen(names[i]), "%llx %c", &value, &extra) != 1 || value != 0)
                invalid("admission");
            found |= 1u << i;
        }
    }
    if (ferror(stream) || fclose(stream) != 0 || found != 31) invalid("admission");
    struct rlimit limit;
    if (getrlimit(RLIMIT_CORE, &limit) != 0 || limit.rlim_cur != 0 || limit.rlim_max != 0) invalid("admission");
    int flags = fcntl(STDOUT_FILENO, F_GETFL);
    if (flags < 0 || fcntl(STDOUT_FILENO, F_SETFL, flags | O_NONBLOCK) < 0 || signal(SIGPIPE, SIG_IGN) == SIG_ERR)
        invalid("transport");
}

static inline unsigned selected_case(void) {
    char line[80];
    size_t used = 0;
    uint64_t until = mono_ns() + UINT64_C(10000000000);
    int flags = fcntl(STDIN_FILENO, F_GETFL);
    if (flags < 0 || fcntl(STDIN_FILENO, F_SETFL, flags | O_NONBLOCK) < 0) invalid("transport");
    while (used < sizeof line - 1 && mono_ns() < until) {
        char value;
        ssize_t count = read(STDIN_FILENO, &value, 1);
        if (count == 1) {
            if (value == '\n') {
                line[used] = 0;
                for (unsigned i = 0; i < 276; i++) if (strcmp(line, OBS_CASES[i]) == 0) return i;
                invalid("case");
            }
            if (value < 32 || value > 126) invalid("case");
            line[used++] = value;
        } else if (count == 0 || (errno != EAGAIN && errno != EINTR)) {
            invalid("transport");
        }
        struct timespec delay = {0, 100000};
        nanosleep(&delay, NULL);
    }
    invalid("deadline");
    return 0;
}

static inline char control_byte(uint64_t until) {
    char value;
    while (mono_ns() < until) {
        ssize_t count = read(STDIN_FILENO, &value, 1);
        if (count == 1) return value;
        if (count == 0 || (errno != EAGAIN && errno != EINTR)) invalid("transport");
        struct timespec delay = {0, 100000};
        nanosleep(&delay, NULL);
    }
    invalid("deadline");
    return 0;
}

static inline void emit(const char *literal) {
    size_t size = strlen(literal);
    if (size > 1024 || write(STDOUT_FILENO, literal, size) != (ssize_t)size) invalid("transport");
}
#endif
