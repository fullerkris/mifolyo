module github.com/IonelPopJara/search-engine/services/spider

go 1.26.0

toolchain go1.26.9

require (
	github.com/alicebob/miniredis/v2 v2.35.0
	github.com/redis/go-redis/v9 v9.22.0
	github.com/temoto/robotstxt v1.1.2
	github.com/yuin/gopher-lua v1.1.1
	golang.org/x/net v0.60.0
	golang.org/x/text v0.42.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

// Preserve CanonicalURLV1's sealed normalization profile while updating HTTP/2.
// The Unicode generator validates this exact replacement and its checksum.
replace golang.org/x/text v0.42.0 => golang.org/x/text v0.41.0
