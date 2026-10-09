# Artifact construction only: no target/observer RUN instruction.
# GCC 14.3.0/bookworm arm64 manifest; the Python runtime base has no C compiler.
FROM docker.io/library/gcc@sha256:66035d353338cb93b64f621393dc6fecde85258651ca454f0cf36ff2639b1352 AS build
WORKDIR /src
COPY . .
RUN mkdir /out && python3 -B build_tools.py headers /out
RUN cc -O2 -g -Wall -Wextra -Werror -Wformat=2 -fstack-protector-strong -D_FORTIFY_SOURCE=2 -fPIE -static-pie -pthread -Wl,-z,relro,-z,now,--build-id=sha1 -I/out native/target.c -lcrypto -ldl -o /out/target
RUN python3 -B build_tools.py manifest /out
RUN cc -O2 -g -Wall -Wextra -Werror -Wformat=2 -fstack-protector-strong -D_FORTIFY_SOURCE=2 -fPIE -static-pie -pthread -Wl,-z,relro,-z,now,--build-id=sha1 -I/out native/observer.c -lcrypto -ldl -o /out/observer
RUN python3 -B build_tools.py record /out

FROM scratch AS artifacts
COPY --from=build /out/ /

FROM scratch AS target
COPY --from=build /out/target /target
USER 999:999
ENTRYPOINT ["/target"]

FROM scratch AS observer
COPY --from=build /out/observer /observer
USER 999:999
ENTRYPOINT ["/observer"]
