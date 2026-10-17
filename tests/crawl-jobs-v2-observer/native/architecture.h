#ifndef OBS1_ARCHITECTURE_H
#define OBS1_ARCHITECTURE_H
#if !defined(__linux__) || __BYTE_ORDER__ != __ORDER_LITTLE_ENDIAN__ || defined(__ILP32__)
#error "C0 requires a little-endian Linux LP64 ABI"
#endif
_Static_assert(sizeof(void *) == 8 && sizeof(long) == 8, "C0 requires LP64");
#if defined(__aarch64__)
#define OBS_ARCHITECTURE "aarch64"
#define OBS_ELF_MACHINE 183
#elif defined(__x86_64__)
#define OBS_ARCHITECTURE "x86_64"
#define OBS_ELF_MACHINE 62
#else
#error "Unsupported C0 native architecture"
#endif
#endif
