#!/usr/bin/env python3
"""Extract exact small production helpers into an entirely simulated C harness."""
import hashlib
import json
from pathlib import Path

ROOT = Path('/tmp/opencode/m4-c0-x64-runtime-2026-10-12')
OUT = ROOT/'correctness'
PKG = ROOT/'snapshot/tests/crawl-jobs-v2-observer/native'
source = (PKG/'observer.c').read_text()
names = ('request', 'peek', 'stopped_pc', 'instruction_check')
helpers = {}
for name in names:
    start = source.index('static ', source.rfind('\n', 0, source.index(' '+name+'(')))
    end = source.index('\n}', start) + 2
    helpers[name] = source[start:end]
common = (PKG/'common.h').read_text()
witness = common[common.index('struct witness {'):common.index('\n};', common.index('struct witness {'))+3]
prefix = r'''
/* Offline helper probe. No real ptrace symbol, actor main, procfs, or filter load. */
#include <assert.h>
#include <errno.h>
#include <inttypes.h>
#include <setjmp.h>
#include <stddef.h>
#include <stdint.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <elf.h>
#include <sys/ptrace.h>
#include <sys/user.h>
#include <sys/uio.h>
#include "architecture.h"
#define OBS_EFFECTS 1050
#define OBS_READ_BYTES (4u*1024u*1024u)
static jmp_buf refusal;
static const char *reason;
static uint64_t deadline = 100, now, reads;
static unsigned checks, calls, fail_at;
static long answer;
static int error_number, mode;
static unsigned indices[32], writes;
static uint64_t dr[8], virtual6;
static unsigned corrupt;
static _Alignas(8) unsigned char memory[32];
static struct user_regs_struct gp;
static size_t regset_size = sizeof gp;
static uintptr_t test_pc;
static uint64_t expected;
static unsigned instruction_size;
static uint64_t mono_ns(void) { return now; }
static void invalid(const char *why) __attribute__((noreturn));
static void invalid(const char *why) { reason = why; longjmp(refusal, 1); }
static long simulated_ptrace(unsigned op, int tid, void *addr, void *data) {
    assert(tid == 1); calls++;
    if (fail_at && calls == fail_at) { errno = EIO; return -1; }
    assert(errno == 0);
    uintptr_t address = (uintptr_t)addr, value = (uintptr_t)data;
    if (mode == 0) { errno = error_number; return answer; }
    if (mode == 1) {
        assert(op == PTRACE_PEEKDATA && address % 8 == 0);
        assert(address >= (uintptr_t)memory && address+8 <= (uintptr_t)memory+sizeof memory);
        uint64_t result; memcpy(&result, (void *)address, 8); return (long)result;
    }
    if (mode == 2) {
        assert(address >= 848 && address <= 904 && (address-848)%8 == 0);
        unsigned i = (address-848)/8; assert(i != 4 && i != 5);
        if (op == PTRACE_PEEKUSER) return (long)((i == 6 ? virtual6 ^ UINT64_C(0xffff0ff0) : dr[i]) ^ (corrupt == i+1));
        assert(op == PTRACE_POKEUSER && (i == 0 || i == 6 || i == 7));
        assert(writes < 32); indices[writes++] = i;
        if (i == 6) virtual6 = value ^ UINT64_C(0xffff0ff0); else dr[i] = value;
        return 0;
    }
    assert(mode == 3 && op == PTRACE_GETREGSET && address == NT_PRSTATUS);
    struct iovec *io = data; assert(io->iov_len == sizeof gp);
    memcpy(io->iov_base, &gp, sizeof gp); io->iov_len = regset_size; return 0;
}
#define ptrace simulated_ptrace
'''
middle = '\n'.join([helpers['request'], helpers['peek'], '#include "x86_debug.h"', helpers['stopped_pc'], helpers['instruction_check'], witness])
suffix = r'''
static void must_pass(void (*op)(void)) {
    reason = NULL;
    if (setjmp(refusal)) { fprintf(stderr,"unexpected refusal: %s\n",reason); abort(); }
    op(); checks++;
}
static void must_fail(void (*op)(void), const char *why) {
    reason = NULL;
    if (!setjmp(refusal)) { op(); assert(!"unexpected acceptance"); }
    assert(reason && strcmp(reason,why) == 0); checks++;
}
static void check_instruction(void) { instruction_check(test_pc,expected,instruction_size); }
static void check_regs(void) { assert(stopped_pc() == test_pc); }
static void check_request(void) { assert(request(PTRACE_PEEKDATA, 0, 0) == answer); }
static void arm(void) { breakpoint(test_pc,1); }
static void restore(void) { breakpoint(test_pc,0); }
static void hit(void) { debug_hit(test_pc); }
static void reset(void) {
    memset(dr,0,sizeof dr); virtual6=0; writes=calls=fail_at=corrupt=0; mode=2; now=0; deadline=100;
}
static void write_status(uint64_t raw) { virtual6 = raw ^ UINT64_C(0xffff0ff0); }
int main(void) {
    assert(sizeof(long)==8 && sizeof(void*)==8 && sizeof(struct user_regs_struct)==216);
    assert(offsetof(struct user_regs_struct,rip)==128 && offsetof(struct user_regs_struct,cs)==136);
    assert(sizeof(struct witness)==8480 && _Alignof(struct witness)==8);
    assert(offsetof(struct witness,completed)==8 && offsetof(struct witness,last_value)==72 && offsetof(struct witness,transcript)==80);
    mode=0; answer=-1; error_number=0; must_pass(check_request);
    error_number=EPERM; must_fail(check_request,"ptrace_denied");
    answer=0; error_number=0; must_pass(check_request);
    unsigned before=calls; now=deadline; must_fail(check_request,"deadline"); assert(calls==before); now=0;
    mode=1;
    for(unsigned offset=0;offset<8;offset++) for(unsigned size=1;size<=8;size++) {
        for(unsigned i=0;i<sizeof memory;i++) memory[i]=(unsigned char)(i*73+39);
        test_pc=(uintptr_t)(memory+8+offset); instruction_size=size; expected=0;
        for(unsigned i=0;i<size;i++) expected |= (uint64_t)memory[8+offset+i]<<(i*8);
        must_pass(check_instruction);
        for(unsigned bit=0;bit<size*8;bit++) { expected ^= UINT64_C(1)<<bit; must_fail(check_instruction,"instruction"); expected ^= UINT64_C(1)<<bit; }
        memory[8+offset-1] ^= 0xff; memory[8+offset+size] ^= 0xff; must_pass(check_instruction);
    }
    instruction_size=0; must_fail(check_instruction,"instruction");
    instruction_size=9; must_fail(check_instruction,"instruction");
    instruction_size=8; test_pc=UINTPTR_MAX-7; must_fail(check_instruction,"instruction");
    test_pc=(uintptr_t)(memory+8); reads=OBS_READ_BYTES; must_fail(check_instruction,"bounds"); reads=0;
    unsigned memory_checks=checks;
    const unsigned regs[]={0,1,2,3,6,7};
    for(unsigned r=0;r<6;r++) for(unsigned bit=0;bit<64;bit++) {
        reset(); unsigned i=regs[r];
        if(i==6) virtual6=UINT64_C(1)<<bit; else dr[i]=UINT64_C(1)<<bit;
        must_fail(debug_capture,"debug_state");
    }
    test_pc=UINT64_C(0x7fff00123ffd);
    reset(); must_pass(debug_capture); must_pass(arm);
    assert(writes==4 && indices[0]==7 && indices[1]==0 && indices[2]==6 && indices[3]==7);
    assert(dr[0]==test_pc && dr[7]==1 && virtual6==0);
    write_status(0xffff0ff1); must_pass(hit);
    for(unsigned r=0;r<6;r++) for(unsigned bit=0;bit<64;bit++) {
        unsigned i=regs[r]; if(i==6) virtual6 ^= UINT64_C(1)<<bit; else dr[i] ^= UINT64_C(1)<<bit;
        must_fail(hit,"debug_state");
        if(i==6) virtual6 ^= UINT64_C(1)<<bit; else dr[i] ^= UINT64_C(1)<<bit;
    }
    must_pass(restore); assert(dr[0]==0 && dr[7]==0 && virtual6==0);
    write_status(0xffff4ff0); must_pass(debug_step);
    for(unsigned r=0;r<6;r++) for(unsigned bit=0;bit<64;bit++) {
        unsigned i=regs[r]; if(i==6) virtual6 ^= UINT64_C(1)<<bit; else dr[i] ^= UINT64_C(1)<<bit;
        must_fail(debug_step,"debug_state");
        if(i==6) virtual6 ^= UINT64_C(1)<<bit; else dr[i] ^= UINT64_C(1)<<bit;
    }
    must_pass(arm); must_pass(restore); assert(dr[0]==0 && dr[7]==0 && virtual6==0);
    for(unsigned i=1;i<=10;i++) {
        reset(); must_pass(debug_capture); calls=0; fail_at=i; must_fail(arm,"ptrace_denied");
    }
    for(unsigned r=0;r<6;r++) { reset(); must_pass(debug_capture); corrupt=regs[r]+1; must_fail(arm,"debug_state"); }
    unsigned debug_checks=checks-memory_checks;
    mode=3; fail_at=0; test_pc=0x7fff00123fff; memset(&gp,0,sizeof gp); gp.rip=test_pc; gp.cs=0x33;
    must_pass(check_regs);
    for(unsigned bit=0;bit<64;bit++) { gp.cs ^= UINT64_C(1)<<bit; must_fail(check_regs,"regset"); gp.cs ^= UINT64_C(1)<<bit; }
    const size_t sizes[]={0,4,108,128,215,217,256};
    for(unsigned i=0;i<sizeof sizes/sizeof *sizes;i++) { regset_size=sizes[i]; must_fail(check_regs,"regset"); }
    printf("{\"status\":\"PASS\",\"checks\":%u,\"request_instruction_checks\":%u,\"debug_checks\":%u,\"regset_checks\":%u,\"ptrace_simulated\":true,\"native_actor_executed\":false,\"user_regs_bytes\":216,\"witness_bytes\":8480}\n",checks,memory_checks,debug_checks,checks-memory_checks-debug_checks);
    return 0;
}
'''
(OUT/'helper_probe.c').write_text(prefix+middle+suffix)
(OUT/'helper-provenance.json').write_text(json.dumps({'observer_sha256': hashlib.sha256((PKG/'observer.c').read_bytes()).hexdigest(),
    'exact_extracted_helpers': {name: hashlib.sha256(text.encode()).hexdigest() for name,text in helpers.items()},
    'debug_header': str(PKG/'x86_debug.h'), 'ptrace_replacement': 'simulated_ptrace',
    'native_actor_main_included': False}, sort_keys=True,indent=2)+'\n')
print('Generated isolated helper probe; all requests simulated.')
