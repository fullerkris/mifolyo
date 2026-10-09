/* Synthetic target only. No Redis, Lua, user expressions, or tracing workaround. */
#include "common.h"
#include "admission.h"
#include <pthread.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/un.h>

volatile uintptr_t obs_witness_address;
extern void obs_effect(_Atomic uint64_t *, uint64_t *, uint64_t, _Atomic uint64_t *);
extern void obs_vm_marker(void);
extern void obs_send_marker(void);

__asm__(".text\n.balign 4\n.global obs_effect\n.type obs_effect,%function\n"
        "obs_effect:\n.global obs_pre_effect\nobs_pre_effect:\n"
        "mov x4,#42\nstr x4,[x3]\nstr x2,[x1]\nstlr x2,[x0]\n.global obs_post_effect\nobs_post_effect:\nret\n"
        ".size obs_effect,.-obs_effect\n"
        ".global obs_vm_marker\n.type obs_vm_marker,%function\nobs_vm_marker:\nnop\nret\n"
        ".size obs_vm_marker,.-obs_vm_marker\n"
        ".global obs_send_marker\n.type obs_send_marker,%function\nobs_send_marker:\nnop\nret\n"
        ".size obs_send_marker,.-obs_send_marker\n");

static void *heartbeat(void *context) {
    struct witness *state = context;
    while (!atomic_load_explicit(&state->finished, memory_order_acquire)) {
        atomic_fetch_add_explicit(&state->heartbeat, 1, memory_order_release);
        struct timespec delay = {0, 100000};
        nanosleep(&delay, NULL);
    }
    return NULL;
}

static void fake_vm(struct witness *state, unsigned prototype, unsigned *ordinal) {
    const unsigned pcs[] = {0, 1, 7};
    for (unsigned i = 0; i < 3; i++) {
        atomic_store(&state->prototype, prototype);
        atomic_store(&state->bytecode_pc, pcs[i]);
        atomic_store(&state->ordinal, ++*ordinal);
        atomic_store(&state->pre_ns, mono_ns());
        obs_vm_marker();
        atomic_store(&state->post_ns, mono_ns());
        if (prototype == 1 && i == 1) fake_vm(state, 2, ordinal);
    }
}

static int stream_peer(void) {
    int listener = socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
    if (listener < 0) invalid("transport");
    struct sockaddr_un address = {.sun_family = AF_UNIX};
    memcpy(address.sun_path, "/control/frame.sock", sizeof "/control/frame.sock");
    if (bind(listener, (struct sockaddr *)&address, sizeof address) != 0 || chmod(address.sun_path, 0600) != 0 || listen(listener, 1) != 0)
        invalid("transport");
    uint64_t until = mono_ns() + OBS_ACTIVE_NS;
    int peer = -1;
    while (mono_ns() < until) {
        peer = accept4(listener, NULL, NULL, SOCK_NONBLOCK | SOCK_CLOEXEC);
        if (peer >= 0) break;
        if (errno != EAGAIN && errno != EINTR) invalid("transport");
        struct timespec delay = {0, 100000};
        nanosleep(&delay, NULL);
    }
    close(listener);
    if (peer < 0) invalid("deadline");
    return peer;
}

int main(int argc, char **argv) {
    (void)argv;
    if (argc != 1 || getpid() != 1) invalid("admission");
    caps_zero();
    unsigned selected = selected_case();
    if (selected >= 264) invalid("validator_control");
    admission_nonce();
    admission_collect(selected, "target");
    int fd = open("/witness/state", O_RDWR | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0600);
    struct stat metadata;
    if (fd < 0 || fstat(fd, &metadata) != 0 || !S_ISREG(metadata.st_mode) || metadata.st_nlink != 1 ||
        metadata.st_uid != 999 || metadata.st_gid != 999 || (metadata.st_mode & 0777) != 0600 ||
        ftruncate(fd, sizeof(struct witness)) != 0) invalid("witness");
    struct witness *state = mmap(NULL, sizeof *state, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    if (state == MAP_FAILED) invalid("witness");
    close(fd);
    memset(state, 0, sizeof *state);
    state->magic = OBS_MAGIC;
    obs_witness_address = (uintptr_t)state;
    emit("{\"phase\":\"READY\",\"version\":1}\n");
    if (control_byte(admission_until) != 'G') invalid("transport");
    uint64_t until = mono_ns() + OBS_ACTIVE_NS;
    pthread_t helper;
    int has_helper = selected >= 200 && selected < 210;
    if (has_helper && pthread_create(&helper, NULL, heartbeat, state) != 0) invalid("helper");
    if (selected >= 140 && selected < 200) {
        unsigned ordinal = 0;
        fake_vm(state, 1, &ordinal);
    } else if (selected >= 210 && selected < 240) {
        int peer = stream_peer();
        unsigned char frame[1024];
        memset(frame, 0x43, sizeof frame);
        if (selected >= 220) {
            size_t before = selected < 230 ? sizeof frame : sizeof frame - 1;
            if (send(peer, frame, before, MSG_NOSIGNAL) != (ssize_t)before) invalid("transport");
        }
        atomic_store(&state->ordinal, 1);
        atomic_store(&state->pre_ns, mono_ns());
        obs_send_marker();
        atomic_store(&state->post_ns, mono_ns());
        if (selected < 220 && send(peer, frame, sizeof frame, MSG_NOSIGNAL) != (ssize_t)sizeof frame) invalid("transport");
        if (selected >= 230 && send(peer, frame, 1, MSG_NOSIGNAL) != 1) invalid("transport");
        close(peer);
    } else {
        for (unsigned i = 1; i <= OBS_EFFECTS; i++) {
            if (mono_ns() >= until) invalid("deadline");
            atomic_store(&state->ordinal, i);
            atomic_store(&state->pre_ns, mono_ns());
            obs_effect(&state->completed, &state->transcript[i - 1], i, &state->last_value);
            atomic_store(&state->post_ns, mono_ns());
        }
    }
    atomic_store_explicit(&state->finished, 1, memory_order_release);
    if (has_helper && pthread_join(helper, NULL) != 0) invalid("helper");
    emit("{\"phase\":\"FINISHED\",\"version\":1}\n");
    if (munmap(state, sizeof *state) != 0) invalid("witness");
    return 0;
}
