#ifndef OBS1_ADMISSION_H
#define OBS1_ADMISSION_H
/* Fixed-path, read-only native observations. No caller PID/path or policy setters. */
#include <dirent.h>
#include <openssl/evp.h>
#include <stdarg.h>
#include <sys/syscall.h>
#include <sys/utsname.h>

static char admission_invocation[65];
static uint64_t admission_until;

static void admission_deadline(void) {
    if (mono_ns() >= admission_until) invalid("deadline");
}

static void admission_nonce(void) {
    admission_until = mono_ns() + UINT64_C(10000000000);
    unsigned nonzero = 0;
    for (unsigned i = 0; i < 64; i++) {
        char byte = control_byte(admission_until);
        if (!((byte >= '0' && byte <= '9') || (byte >= 'a' && byte <= 'f'))) invalid("admission");
        nonzero |= byte != '0';
        admission_invocation[i] = byte;
    }
    admission_invocation[64] = 0;
    if (!nonzero || control_byte(admission_until) != '\n') invalid("admission");
}

static size_t admission_read(const char *path, char *raw, size_t capacity) {
    admission_deadline();
    int fd = open(path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
    if (fd < 0) invalid("admission");
    size_t used = 0;
    for (;;) {
        admission_deadline();
        ssize_t count = read(fd, raw + used, capacity - 1 - used);
        if (count < 0) invalid("admission");
        if (!count) break;
        used += (size_t)count;
        if (used == capacity - 1) invalid("bounds");
    }
    if (close(fd) || !used || memchr(raw, 0, used)) invalid("admission");
    raw[used] = 0;
    return used;
}

static void admission_hash(const void *raw, size_t size, char output[65]) {
    unsigned char result[32];
    unsigned length = 0;
    if (EVP_Digest(raw, size, result, &length, EVP_sha256(), NULL) != 1 || length != 32) invalid("admission");
    for (unsigned i = 0; i < 32; i++) snprintf(output + 2 * i, 3, "%02x", result[i]);
    output[64] = 0;
}

static void admission_file_hash(const char *path, char output[65]) {
    char raw[4096];
    size_t size = admission_read(path, raw, sizeof raw);
    admission_hash(raw, size, output);
}

static uint64_t admission_number(const char *raw) {
    unsigned long long value;
    char extra;
    if (raw[0] < '0' || raw[0] > '9' || sscanf(raw, "%llu %c", &value, &extra) != 1 || value > UINT64_C(9007199254740991)) invalid("admission");
    return value;
}

static uint64_t admission_limit(const char *path) {
    char raw[128];
    admission_read(path, raw, sizeof raw);
    return admission_number(raw);
}

static uint64_t admission_ticks(void) {
    char raw[4096], *save;
    admission_read("/proc/self/stat", raw, sizeof raw);
    char *end = strrchr(raw, ')');
    if (!end || end[1] != ' ') invalid("admission");
    char *part = strtok_r(end + 2, " ", &save);
    for (unsigned field = 3; field < 22 && part; field++) part = strtok_r(NULL, " ", &save);
    if (!part) invalid("admission");
    uint64_t ticks = admission_number(part);
    if (!ticks) invalid("admission");
    return ticks;
}

static void admission_namespaces(uint64_t values[7], uint64_t *device) {
    const char *names[] = {"pid", "user", "time", "cgroup", "mnt", "net", "ipc"};
    for (unsigned i = 0; i < 7; i++) {
        char path[64];
        struct stat info;
        int length = snprintf(path, sizeof path, "/proc/self/ns/%s", names[i]);
        if (length <= 0 || (size_t)length >= sizeof path || stat(path, &info) || !info.st_ino) invalid("admission");
        if (!i) *device = info.st_dev;
        else if (*device != (uint64_t)info.st_dev) invalid("admission");
        values[i] = info.st_ino;
    }
}

static void admission_executable(char output[65]) {
    /* /proc/self/exe is deliberately followed; every other admission file is a fixed leaf. */
    int fd = open("/proc/self/exe", O_RDONLY | O_CLOEXEC);
    struct stat before, after;
    if (fd < 0 || fstat(fd, &before) || !S_ISREG(before.st_mode) || before.st_size <= 0 || before.st_size > 32 * 1024 * 1024) invalid("admission");
    EVP_MD_CTX *ctx = EVP_MD_CTX_new();
    unsigned char raw[32768], result[32];
    unsigned length;
    size_t total = 0;
    if (!ctx || EVP_DigestInit_ex(ctx, EVP_sha256(), NULL) != 1) invalid("admission");
    for (;;) {
        admission_deadline();
        ssize_t count = read(fd, raw, sizeof raw);
        if (count < 0) invalid("admission");
        if (!count) break;
        total += (size_t)count;
        if (total > (size_t)before.st_size || EVP_DigestUpdate(ctx, raw, (size_t)count) != 1) invalid("admission");
    }
    if (total != (size_t)before.st_size || fstat(fd, &after) || before.st_dev != after.st_dev || before.st_ino != after.st_ino ||
        before.st_size != after.st_size || before.st_mtim.tv_sec != after.st_mtim.tv_sec || before.st_mtim.tv_nsec != after.st_mtim.tv_nsec ||
        EVP_DigestFinal_ex(ctx, result, &length) != 1 || length != 32 || close(fd)) invalid("admission");
    EVP_MD_CTX_free(ctx);
    for (unsigned i = 0; i < 32; i++) snprintf(output + 2 * i, 3, "%02x", result[i]);
    output[64] = 0;
}

static unsigned admission_security(unsigned *groups) {
    char raw[8192], *save;
    admission_read("/proc/self/status", raw, sizeof raw);
    unsigned found = 0, filters = 0;
    const char *keys[] = {"Uid:", "Gid:", "Groups:", "NoNewPrivs:", "Seccomp:", "Seccomp_filters:", "Threads:", "TracerPid:"};
    for (char *line = strtok_r(raw, "\n", &save); line; line = strtok_r(NULL, "\n", &save)) {
        for (unsigned i = 0; i < 8; i++) {
            if (strncmp(line, keys[i], strlen(keys[i]))) continue;
            if (found & (1u << i)) invalid("admission");
            found |= 1u << i;
            char *value = line + strlen(keys[i]);
            while (*value == ' ' || *value == '\t') value++;
            if (i < 2) {
                unsigned a, b, c, d; char extra;
                if (sscanf(value, "%u %u %u %u %c", &a, &b, &c, &d, &extra) != 4 || a != 999 || b != 999 || c != 999 || d != 999) invalid("admission");
            } else if (i == 2) {
                *groups = *value ? 1 : 0;
                if (*groups && admission_number(value) != 999) invalid("admission");
            } else {
                uint64_t number = admission_number(value);
                if (i == 5) {
                    if (!number || number > 16) invalid("admission");
                    filters = (unsigned)number;
                } else if (number != (i == 4 ? 2u : i == 7 ? 0u : 1u)) invalid("admission");
            }
        }
    }
    if (found != 255 || prctl(PR_GET_DUMPABLE, 0, 0, 0, 0) != 1 || prctl(PR_GET_SECCOMP, 0, 0, 0, 0) != 2) invalid("admission");
    DIR *directory = opendir("/proc/self/task");
    if (!directory) invalid("admission");
    struct dirent *entry;
    unsigned count = 0;
    errno = 0;
    while ((entry = readdir(directory))) {
        if (!strcmp(entry->d_name, ".") || !strcmp(entry->d_name, "..")) continue;
        if (++count != 1 || admission_number(entry->d_name) != (uint64_t)getpid()) invalid("admission");
    }
    if (errno || closedir(directory) || count != 1) invalid("admission");
    return filters;
}

static void admission_emit(const char *format, ...) __attribute__((format(printf, 1, 2)));
static void admission_emit(const char *format, ...) {
    char raw[1025];
    va_list args;
    va_start(args, format);
    int length = vsnprintf(raw, sizeof raw, format, args);
    va_end(args);
    if (length <= 0 || length > 1024) invalid("bounds");
    admission_deadline();
    emit(raw);
}

static void admission_collect(unsigned selected, const char *role) {
    struct utsname kernel;
    if (uname(&kernel) || strcmp(kernel.sysname, "Linux") || strcmp(kernel.machine, "aarch64")) invalid("admission");
    caps_zero();
    uint64_t pid = (uint64_t)getpid(), tid = (uint64_t)syscall(SYS_gettid), start = admission_ticks(), ns[7], device;
    if (pid != tid) invalid("admission");
    admission_namespaces(ns, &device);
    char exe[65], boot[65], uid[65], gid[65], cgroup[65], release[65], lsm[65], offsets[65], raw[4096];
    admission_executable(exe);
    admission_file_hash("/proc/sys/kernel/random/boot_id", boot);
    admission_file_hash("/proc/self/uid_map", uid);
    admission_file_hash("/proc/self/gid_map", gid);
    admission_file_hash("/proc/self/cgroup", cgroup);
    admission_read("/proc/self/cgroup", raw, sizeof raw);
    if (strcmp(raw, "0::/\n")) invalid("admission");
    admission_hash(kernel.release, strlen(kernel.release), release);
    admission_file_hash("/sys/kernel/security/lsm", lsm);
    admission_file_hash("/proc/self/timens_offsets", offsets);
    admission_read("/proc/self/timens_offsets", raw, sizeof raw);
    long long mono_s, boot_s; unsigned mono_ns_part, boot_ns_part; char extra;
    if (sscanf(raw, "monotonic %lld %u boottime %lld %u %c", &mono_s, &mono_ns_part, &boot_s, &boot_ns_part, &extra) != 4 ||
        mono_s || boot_s || mono_ns_part || boot_ns_part) invalid("admission");
    uint64_t yama = admission_limit("/proc/sys/kernel/yama/ptrace_scope");
    if (yama > 3) invalid("admission");
    unsigned groups = 0, filters = admission_security(&groups);
    uint64_t memory = admission_limit("/sys/fs/cgroup/memory.max"), swap = admission_limit("/sys/fs/cgroup/memory.swap.max");
    uint64_t current = admission_limit("/sys/fs/cgroup/memory.current"), peak = admission_limit("/sys/fs/cgroup/memory.peak");
    uint64_t pids = admission_limit("/sys/fs/cgroup/pids.max"), pids_current = admission_limit("/sys/fs/cgroup/pids.current");
    uint64_t process = admission_limit("/sys/fs/cgroup/cgroup.procs");
    unsigned long long quota, period;
    admission_read("/sys/fs/cgroup/cpu.max", raw, sizeof raw);
    if (sscanf(raw, "%llu %llu %c", &quota, &period, &extra) != 2 || quota != period || period < 1000 || period > 1000000 ||
        memory != 67108864 || swap || !current || current > peak || peak > memory || pids != 16 || pids_current != 1 || process != pid) invalid("admission");
    struct timespec resolution;
    if (clock_getres(CLOCK_MONOTONIC, &resolution) || resolution.tv_sec || resolution.tv_nsec <= 0 || resolution.tv_nsec > 1000000) invalid("admission");
    uint64_t after[7], after_device;
    admission_namespaces(after, &after_device);
    if (memcmp(ns, after, sizeof ns) || device != after_device || admission_ticks() != start) invalid("identity");
    unsigned after_groups = 0;
    if (admission_security(&after_groups) != filters || after_groups != groups) invalid("identity");
    char again[65];
    admission_file_hash("/proc/self/uid_map", again);
    if (strcmp(again, uid)) invalid("identity");
    admission_file_hash("/proc/self/gid_map", again);
    if (strcmp(again, gid)) invalid("identity");
    caps_zero();
    const char *case_id = OBS_CASES[selected];
    admission_emit("{\"boot_sha256\":\"%s\",\"case_id\":\"%s\",\"cgroup_ns\":%" PRIu64 ",\"executable_sha256\":\"%s\",\"gid_map_sha256\":\"%s\",\"invocation_sha256\":\"%s\",\"ipc_ns\":%" PRIu64 ",\"mnt_ns\":%" PRIu64 ",\"net_ns\":%" PRIu64 ",\"ns_device\":%" PRIu64 ",\"phase\":\"IDENTITY\",\"pid\":%" PRIu64 ",\"pid_ns\":%" PRIu64 ",\"role\":\"%s\",\"start_ticks\":%" PRIu64 ",\"tid\":%" PRIu64 ",\"time_ns\":%" PRIu64 ",\"uid_map_sha256\":\"%s\",\"user_ns\":%" PRIu64 ",\"version\":1}\n",
        boot, case_id, ns[3], exe, gid, admission_invocation, ns[6], ns[4], ns[5], device, pid, ns[0], role, start, tid, ns[2], uid, ns[1]);
    admission_emit("{\"caps\":[0,0,0,0,0],\"case_id\":\"%s\",\"core_hard\":0,\"core_soft\":0,\"dumpable\":1,\"gids\":[999,999,999,999],\"groups\":[%s],\"invocation_sha256\":\"%s\",\"nnp\":1,\"phase\":\"CONFINEMENT\",\"role\":\"%s\",\"seccomp\":2,\"seccomp_filters\":%u,\"tasks\":[%" PRIu64 "],\"threads\":1,\"tracer_pid\":0,\"uids\":[999,999,999,999],\"version\":1}\n", case_id, groups ? "999" : "", admission_invocation, role, filters, pid);
    admission_emit("{\"case_id\":\"%s\",\"cgroup_sha256\":\"%s\",\"cpu_period\":%llu,\"cpu_quota\":%llu,\"invocation_sha256\":\"%s\",\"memory_current\":%" PRIu64 ",\"memory_max\":%" PRIu64 ",\"memory_peak\":%" PRIu64 ",\"phase\":\"RESOURCES\",\"pids_current\":%" PRIu64 ",\"pids_max\":%" PRIu64 ",\"processes\":[%" PRIu64 "],\"role\":\"%s\",\"swap_max\":%" PRIu64 ",\"version\":1}\n", case_id, cgroup, period, quota, admission_invocation, current, memory, peak, pids_current, pids, process, role, swap);
    admission_emit("{\"case_id\":\"%s\",\"invocation_sha256\":\"%s\",\"kernel_release_sha256\":\"%s\",\"lsm_sha256\":\"%s\",\"monotonic_ns\":%" PRIu64 ",\"phase\":\"CLOCK\",\"resolution_ns\":%" PRIu64 ",\"role\":\"%s\",\"time_offsets_sha256\":\"%s\",\"version\":1,\"yama_scope\":%" PRIu64 "}\n", case_id, admission_invocation, release, lsm, mono_ns(), (uint64_t)resolution.tv_nsec, role, offsets, yama);
}
#endif
