#ifndef OBS1_X86_DEBUG_H
#define OBS1_X86_DEBUG_H
/* Included after the bounded request() wrapper. This interface never writes GP
   registers, target memory or DR1..5; all requests address the owned TID 1. */
#include <asm/debugreg.h>
#include <sys/user.h>
_Static_assert(offsetof(struct user, u_debugreg[0]) == 848 &&
    offsetof(struct user, u_debugreg[3]) == 872 &&
    offsetof(struct user, u_debugreg[6]) == 896 &&
    offsetof(struct user, u_debugreg[7]) == 904 && sizeof(((struct user *)0)->u_debugreg[0]) == 8,
    "closed x86-64 debug-register offsets (must match profiles.py)");
_Static_assert(sizeof(struct user_regs_struct) == 216 && DR6_RESERVED == 0xffff0ff0 && DR_LOCAL_ENABLE == 1,
    "closed x86-64 register ABI");

static uint64_t saved_debug[8];

static uint64_t debug_read(unsigned index) {
    return (uint64_t)request(PTRACE_PEEKUSER, offsetof(struct user, u_debugreg) + 8 * index, 0);
}

static void debug_write(unsigned index, uint64_t value) {
    if (index != 0 && index != 6 && index != 7) invalid("debug_state");
    request(PTRACE_POKEUSER, offsetof(struct user, u_debugreg) + 8 * index, value);
}

static void debug_verify(uintptr_t pc, uint64_t status, uint64_t control) {
    if (debug_read(0) != pc || debug_read(6) != status || debug_read(7) != control) invalid("debug_state");
    for (unsigned i = 1; i < 4; i++) if (debug_read(i)) invalid("debug_state");
}

static void debug_capture(void) {
    for (unsigned i = 0; i < 8; i++) {
        if (i == 4 || i == 5) continue;  /* Reserved aliases are never accessed. */
        saved_debug[i] = debug_read(i);
        if (saved_debug[i] != (i == 6 ? DR6_RESERVED : 0)) invalid("debug_state");
    }
}

static void breakpoint(uintptr_t pc, int enabled) {
    /* Disable before changing address/status, then verify the kernel readback.
       Restore the exact admitted inactive state before single-step or detach. */
    debug_write(7, 0);
    debug_write(0, enabled ? pc : saved_debug[0]);
    debug_write(6, saved_debug[6]);
    debug_write(7, enabled ? DR_LOCAL_ENABLE : saved_debug[7]);
    debug_verify(enabled ? pc : saved_debug[0], saved_debug[6], enabled ? DR_LOCAL_ENABLE : saved_debug[7]);
}

static void debug_hit(uintptr_t pc) {
    debug_verify(pc, DR6_RESERVED | DR_TRAP0, DR_LOCAL_ENABLE);
}

static void debug_step(void) {
    debug_verify(saved_debug[0], DR6_RESERVED | DR_STEP, saved_debug[7]);
}
#endif
