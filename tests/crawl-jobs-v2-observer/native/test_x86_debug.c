/* Offline unit probe for the production debug-register helpers. All ptrace
   requests are simulated in memory. No native actor or tracing call executes. */
#include <assert.h>
#include <setjmp.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/ptrace.h>

static jmp_buf refusal;
static uint64_t registers[8];
static unsigned written[32], writes, checks;
static int corrupt_readback;

static void invalid(const char *reason) __attribute__((noreturn));
static void invalid(const char *reason) {
    assert(strcmp(reason, "debug_state") == 0);
    longjmp(refusal, 1);
}

static long request(unsigned operation, uintptr_t address, uintptr_t data) {
    assert(address >= 848 && address <= 904 && (address - 848) % 8 == 0);
    unsigned index = (unsigned)(address - 848) / 8;
    assert(index != 4 && index != 5);
    if (operation == PTRACE_PEEKUSER) return (long)(registers[index] ^ (corrupt_readback && index == 0 ? 1 : 0));
    assert(operation == PTRACE_POKEUSER && (index == 0 || index == 6 || index == 7));
    assert(writes < 32);
    written[writes++] = index;
    registers[index] = data;
    return 0;
}

#include "x86_debug.h"

static void reset(void) {
    memset(registers, 0, sizeof registers);
    registers[6] = 0xffff0ff0;
    writes = 0;
    corrupt_readback = 0;
}

static void rejected(void (*operation)(void)) {
    if (setjmp(refusal) == 0) {
        operation();
        assert(!"unexpected acceptance");
    }
    checks++;
}

static void arm(void) { breakpoint(0x401003, 1); }
static void hit(void) { debug_hit(0x401003); }
static void bad_write(void) { debug_write(1, 0x401003); }

int main(void) {
    const unsigned indices[] = {0, 1, 2, 3, 6, 7};
    for (unsigned i = 0; i < sizeof indices / sizeof indices[0]; i++) {
        reset(); registers[indices[i]] ^= 1;
        rejected(debug_capture);
    }
    reset(); debug_capture(); checks++;
    arm();
    assert(writes == 4 && written[0] == 7 && written[1] == 0 && written[2] == 6 && written[3] == 7);
    assert(registers[0] == 0x401003 && registers[6] == 0xffff0ff0 && registers[7] == 1);
    checks++;
    registers[6] |= 1; hit(); checks++;
    const uint64_t bad_status[] = {0, 0xffff0ff0, 0xffff0ff2, 0xffff2ff1, 0xffff4ff1, 0xffff8ff1, 0xfffe0ff1, 0xffff07f1, UINT64_C(0x1ffff0ff1)};
    for (unsigned i = 0; i < sizeof bad_status / sizeof bad_status[0]; i++) {
        registers[6] = bad_status[i]; rejected(hit);
    }
    registers[6] = 0xffff0ff1;
    for (unsigned i = 0; i < 4; i++) {
        registers[i] ^= 1; rejected(hit); registers[i] ^= 1;
    }
    registers[7] = 3; rejected(hit); registers[7] = 1;
    breakpoint(0x401003, 0);
    assert(registers[0] == 0 && registers[6] == 0xffff0ff0 && registers[7] == 0); checks++;
    registers[6] |= 0x4000; debug_step(); checks++;
    registers[6] |= 1; rejected(debug_step);
    breakpoint(0x401003, 0);
    assert(memcmp(registers, saved_debug, sizeof registers) == 0); checks++;
    corrupt_readback = 1; rejected(arm);
    corrupt_readback = 0; rejected(bad_write);
    printf("PASS: %u offline x86 debug-state controls; ptrace simulated\n", checks);
    return 0;
}
