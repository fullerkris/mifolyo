/* C0-only sibling observer. This binary is built for review, never run by prepare. */
#include "common.h"
#include "admission.h"
#include "target_manifest.h"
#include <asm/ptrace.h>
#include <elf.h>
#include <linux/audit.h>
#include <linux/filter.h>
#include <linux/seccomp.h>
#include <openssl/evp.h>
#include <stddef.h>
#include <sys/ptrace.h>
#include <sys/syscall.h>
#include <sys/sysmacros.h>
#include <sys/uio.h>
#include <sys/wait.h>
#include "target_filter.h"

_Static_assert(__NR_ptrace == 117 && __NR_execve == 221 && __NR_execveat == 281, "closed AArch64 syscall ABI");

static uint64_t deadline, reads, events, start_ticks, load_bias;
static struct stat executable, pid_namespace, user_namespace, time_namespace;
static unsigned slots;
static struct user_hwdebug_state saved_debug;

static void sha_file(int fd, char output[65]) {
    EVP_MD_CTX *context = EVP_MD_CTX_new();
    unsigned char bytes[32768], result[EVP_MAX_MD_SIZE];
    unsigned length = 0;
    size_t total = 0;
    if (!context || EVP_DigestInit_ex(context, EVP_sha256(), NULL) != 1) invalid("identity");
    for (;;) {
        ssize_t count = read(fd, bytes, sizeof bytes);
        if (count < 0) invalid("identity");
        if (count == 0) break;
        total += (size_t)count;
        if (total > 32u * 1024u * 1024u || EVP_DigestUpdate(context, bytes, (size_t)count) != 1) invalid("bounds");
    }
    if (EVP_DigestFinal_ex(context, result, &length) != 1 || length != 32) invalid("identity");
    EVP_MD_CTX_free(context);
    for (unsigned i = 0; i < 32; i++) snprintf(output + 2 * i, 3, "%02x", result[i]);
    output[64] = 0;
}

static uint64_t process_start(void) {
    char raw[4096];
    int fd = open("/proc/1/stat", O_RDONLY | O_CLOEXEC);
    if (fd < 0) invalid("identity");
    ssize_t size = read(fd, raw, sizeof raw - 1);
    close(fd);
    if (size <= 0 || size == (ssize_t)sizeof raw - 1) invalid("identity");
    raw[size] = 0;
    char *end = strrchr(raw, ')'), *state;
    if (!end || end[1] != ' ') invalid("identity");
    char *token = strtok_r(end + 2, " ", &state);
    for (unsigned field = 3; field < 22 && token; field++) token = strtok_r(NULL, " ", &state);
    if (!token) invalid("identity");
    char *last;
    errno = 0;
    unsigned long long value = strtoull(token, &last, 10);
    if (errno || !value || *last) invalid("identity");
    return value;
}

static void namespace_check(const char *target, const char *self, struct stat *initial, int first) {
    struct stat a, b;
    if (stat(target, &a) || stat(self, &b) || a.st_ino != b.st_ino || a.st_dev != b.st_dev) invalid("identity");
    if (first) *initial = a;
    else if (a.st_ino != initial->st_ino || a.st_dev != initial->st_dev) invalid("identity");
}

static uint64_t mappings(int first) {
    FILE *file = fopen("/proc/1/maps", "re");
    if (!file) invalid("identity");
    char line[1024];
    unsigned matches = 0, lines = 0;
    uint64_t bias = 0;
    while (fgets(line, sizeof line, file)) {
        unsigned long long begin, end, offset, inode;
        unsigned major_number, minor_number;
        char access[5];
        if (++lines > 256 || !strchr(line, '\n') ||
            sscanf(line, "%llx-%llx %4s %llx %x:%x %llu", &begin, &end, access, &offset, &major_number, &minor_number, &inode) != 7)
            invalid("identity");
        if (inode != executable.st_ino || major_number != major(executable.st_dev) || minor_number != minor(executable.st_dev)) continue;
        if (access[2] != 'x') continue;
        uint64_t candidate;
        if (access[0] != 'r' || access[1] != '-' || offset != OBS_EXEC_OFFSET ||
            __builtin_sub_overflow((uint64_t)begin, OBS_EXEC_VADDR, &candidate) || end <= begin) invalid("identity");
        if (end - begin < OBS_EXEC_SIZE) invalid("identity");
        bias = candidate;
        matches++;
    }
    if (ferror(file) || fclose(file) || matches != 1 || (!first && bias != load_bias)) invalid("identity");
    return bias;
}

static void identity_check(int first) {
    caps_zero();
    if (getpid() == 1 || process_start() != start_ticks) invalid("identity");
    namespace_check("/proc/1/ns/pid", "/proc/self/ns/pid", &pid_namespace, first);
    namespace_check("/proc/1/ns/user", "/proc/self/ns/user", &user_namespace, first);
    namespace_check("/proc/1/ns/time", "/proc/self/ns/time", &time_namespace, first);
    struct stat current;
    if (stat("/proc/1/exe", &current) || current.st_ino != executable.st_ino || current.st_dev != executable.st_dev ||
        current.st_size != executable.st_size) invalid("identity");
    if (first) load_bias = mappings(1);
    else (void)mappings(0);
}

static void install_target_filter(void) {
    /* Additional filter; the separate default-deny OCI profile is also required.
       PID 1 is observed as the owned namespace init, never caller-selected. */
    struct sock_fprog filter = {.len = sizeof obs_filter / sizeof obs_filter[0], .filter = (struct sock_filter *)obs_filter};
    if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &filter) != 0) invalid("admission");
}

static long request(unsigned operation, uintptr_t address, uintptr_t data) {
    if (mono_ns() >= deadline) invalid("deadline");
    errno = 0;
    long result = ptrace(operation, 1, (void *)address, (void *)data);
    if (result == -1 && errno) invalid("ptrace_denied");
    return result;
}

static uint64_t peek(uintptr_t address) {
    if (address % 8 || reads + 8 > OBS_READ_BYTES) invalid("bounds");
    reads += 8;
    return (uint64_t)request(PTRACE_PEEKDATA, address, 0);
}

static int wait_stop(void) {
    int status;
    while (mono_ns() < deadline) {
        pid_t result = waitpid(1, &status, __WALL | WNOHANG);
        if (result == 1) {
            if (++events > OBS_EVENTS || !WIFSTOPPED(status)) invalid("stop_reason");
            identity_check(0);
            return status;
        }
        if (result < 0 && errno != EINTR) invalid("stop_reason");
        struct timespec delay = {0, 100000};
        nanosleep(&delay, NULL);
    }
    invalid("deadline");
    return 0;
}

static void debug_state(struct user_hwdebug_state *value, int set) {
    struct iovec io = {.iov_base = value, .iov_len = set ? 8 + 16 * slots : sizeof *value};
    request(set ? PTRACE_SETREGSET : PTRACE_GETREGSET, NT_ARM_HW_BREAK, (uintptr_t)&io);
    if (!set && io.iov_len != sizeof *value) invalid("regset");
}

static void breakpoint(uintptr_t pc, int enabled) {
    struct user_hwdebug_state desired = saved_debug;
    if (enabled) {
        desired.dbg_regs[0].addr = pc;
        desired.dbg_regs[0].ctrl = 0x1e5; /* enabled, EL0, execute, four instruction bytes */
    }
    debug_state(&desired, 1);
}

static uintptr_t witness_pointer(void) {
    uintptr_t pointer = peek(load_bias + OBS_WITNESS_SYMBOL);
    if (pointer % 8 || pointer > UINTPTR_MAX - sizeof(struct witness)) invalid("identity");
    FILE *file = fopen("/proc/1/maps", "re");
    if (!file) invalid("identity");
    char line[1024], name[256], access[5];
    unsigned count = 0, lines = 0;
    while (fgets(line, sizeof line, file)) {
        unsigned long long begin, end, offset, inode;
        unsigned maj, min;
        name[0] = 0;
        if (++lines > 256 || !strchr(line, '\n')) invalid("bounds");
        if (sscanf(line, "%llx-%llx %4s %llx %x:%x %llu %255s", &begin, &end, access, &offset, &maj, &min, &inode, name) == 8 &&
            begin == pointer && end >= pointer + sizeof(struct witness) && offset == 0 && inode != 0 &&
            strcmp(name, "/witness/state") == 0 && strcmp(access, "rw-s") == 0) count++;
    }
    if (ferror(file) || fclose(file) || count != 1 || peek(pointer) != OBS_MAGIC) invalid("identity");
    return pointer;
}

int main(int argc, char **argv) {
    (void)argv;
    if (argc != 1 || getpid() == 1) invalid("admission");
    caps_zero();
    unsigned selected = selected_case();
    if (selected >= 264) invalid("validator_control");
    admission_nonce();
    int fd = open("/proc/1/exe", O_RDONLY | O_CLOEXEC);
    char executable_sha[65];
    if (fd < 0 || fstat(fd, &executable) || !S_ISREG(executable.st_mode) || executable.st_size != OBS_TARGET_SIZE) invalid("identity");
    sha_file(fd, executable_sha);
    close(fd);
    if (strcmp(executable_sha, OBS_TARGET_SHA256) != 0) invalid("identity");
    start_ticks = process_start();
    identity_check(1);
    install_target_filter();
    admission_collect(selected, "observer");
    emit("{\"phase\":\"ADMISSION_READY\",\"version\":1}\n");
    if (control_byte(admission_until) != 'A') invalid("transport");
    identity_check(0);
    deadline = mono_ns() + OBS_ACTIVE_NS;
    /* TRACEEXEC adds a rejection stop, not an exec-following mode. Every exec
       event is invalid; no re-seize or reattachment occurs. */
    request(PTRACE_SEIZE, 0, PTRACE_O_EXITKILL | PTRACE_O_TRACEEXEC);
    request(PTRACE_INTERRUPT, 0, 0);
    int status = wait_stop();
    if (WSTOPSIG(status) != SIGTRAP || (unsigned)status >> 16 != PTRACE_EVENT_STOP) invalid("stop_reason");
    memset(&saved_debug, 0, sizeof saved_debug);
    debug_state(&saved_debug, 0);
    slots = saved_debug.dbg_info & 255;
    if (slots < 1 || slots > 16) invalid("regset");
    for (unsigned i = 0; i < 16; i++) if (saved_debug.dbg_regs[i].addr || saved_debug.dbg_regs[i].ctrl) invalid("debug_state");
    const unsigned ordinals[] = {1, 2, 127, 128, 129, 1049, 1050};
    unsigned wanted = selected < 140 ? ordinals[(selected % 70) / 10] : selected < 240 ? 1 : 129;
    uintptr_t relative = selected < 140 ? (selected < 70 ? OBS_PRE_EFFECT : OBS_POST_EFFECT) :
        selected < 200 ? OBS_VM_MARKER : selected >= 210 && selected < 240 ? OBS_SEND_MARKER : OBS_PRE_EFFECT;
    uintptr_t pc = load_bias + relative;
    uint32_t instruction = (uint32_t)(peek(pc & ~(uintptr_t)7) >> ((pc & 7) * 8));
    uint32_t expected = selected < 140 ? (selected < 70 ? OBS_PRE_INSTRUCTION : OBS_POST_INSTRUCTION) : OBS_NOP_INSTRUCTION;
    if (selected >= 200 && !(selected >= 210 && selected < 240)) expected = OBS_PRE_INSTRUCTION;
    if (instruction != expected) invalid("instruction");
    breakpoint(pc, 1);
    emit("{\"phase\":\"ARMED\",\"version\":1}\n");
    request(PTRACE_CONT, 0, 0);
    for (;;) {
        status = wait_stop();
        siginfo_t signal;
        struct user_pt_regs registers;
        struct iovec regs = {.iov_base = &registers, .iov_len = sizeof registers};
        if (WSTOPSIG(status) != SIGTRAP || (unsigned)status >> 16 != 0) invalid("stop_reason");
        request(PTRACE_GETSIGINFO, 0, (uintptr_t)&signal);
        request(PTRACE_GETREGSET, NT_PRSTATUS, (uintptr_t)&regs);
        if (signal.si_code != TRAP_HWBKPT || regs.iov_len != sizeof registers || registers.pc != pc) invalid("stop_reason");
        uintptr_t pointer = witness_pointer();
        uint64_t ordinal = peek(pointer + offsetof(struct witness, ordinal));
        int match = ordinal == wanted;
        if (selected >= 140 && selected < 200) {
            const unsigned pcs[] = {0, 1, 7};
            match = peek(pointer + offsetof(struct witness, prototype)) == 1 + (selected - 140) / 30 &&
                peek(pointer + offsetof(struct witness, bytecode_pc)) == pcs[((selected - 140) % 30) / 10];
        }
        if (match) {
            uint64_t pre = peek(pointer + offsetof(struct witness, pre_ns));
            uint64_t acquired = mono_ns();
            if (pre > acquired || acquired - pre >= OBS_HOLD_NS) invalid("deadline");
            char output[256];
            int length = snprintf(output, sizeof output,
                "{\"events\":%" PRIu64 ",\"ordinal\":%" PRIu64 ",\"phase\":\"HELD\",\"read_bytes\":%" PRIu64 ",\"version\":1}\n",
                events, ordinal, reads);
            if (length < 1 || (size_t)length >= sizeof output) invalid("bounds");
            emit(output);
            char action = control_byte(pre + OBS_HOLD_NS);
            if (action != 'R') invalid("controller_kill_required");
            breakpoint(pc, 0);
            request(PTRACE_DETACH, 0, 0);
            emit("{\"phase\":\"CONTINUATION_DISPATCHED\",\"version\":1}\n");
            /* No timing/cleanup PASS: independent post-continuation/actual-stop
               and complete resource receipts are still mandatory. */
            return 0;
        }
        breakpoint(pc, 0);
        request(PTRACE_SINGLESTEP, 0, 0);
        status = wait_stop();
        if (WSTOPSIG(status) != SIGTRAP || (unsigned)status >> 16 != 0) invalid("stop_reason");
        request(PTRACE_GETSIGINFO, 0, (uintptr_t)&signal);
        if (signal.si_code != TRAP_TRACE) invalid("stop_reason");
        breakpoint(pc, 1);
        request(PTRACE_CONT, 0, 0);
    }
}
