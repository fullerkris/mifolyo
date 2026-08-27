// Package securefetch performs policy-gated, DNS-pinned HTTP fetches for the
// spider. It deliberately does not expose a general-purpose HTTP client: every
// request is matched, admitted by a gate, resolved once, and dialed only by
// numeric address.
package securefetch
