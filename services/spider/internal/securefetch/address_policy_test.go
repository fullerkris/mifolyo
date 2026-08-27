package securefetch

import (
	"net/netip"
	"testing"
)

func TestIPv4ProhibitedRangeBoundariesAndPublicNeighbors(t *testing.T) {
	tests := []struct {
		prefix string
		before string
		after  string
	}{
		{prefix: "0.0.0.0/8", after: "1.0.0.0"},
		{prefix: "10.0.0.0/8", before: "9.255.255.255", after: "11.0.0.0"},
		{prefix: "100.64.0.0/10", before: "100.63.255.255", after: "100.128.0.0"},
		{prefix: "127.0.0.0/8", before: "126.255.255.255", after: "128.0.0.0"},
		{prefix: "169.254.0.0/16", before: "169.253.255.255", after: "169.255.0.0"},
		{prefix: "172.16.0.0/12", before: "172.15.255.255", after: "172.32.0.0"},
		{prefix: "192.0.0.0/24", before: "191.255.255.255", after: "192.0.1.0"},
		{prefix: "192.0.2.0/24", before: "192.0.1.255", after: "192.0.3.0"},
		{prefix: "192.31.196.0/24", before: "192.31.195.255", after: "192.31.197.0"},
		{prefix: "192.52.193.0/24", before: "192.52.192.255", after: "192.52.194.0"},
		{prefix: "192.88.99.0/24", before: "192.88.98.255", after: "192.88.100.0"},
		{prefix: "192.168.0.0/16", before: "192.167.255.255", after: "192.169.0.0"},
		{prefix: "192.175.48.0/24", before: "192.175.47.255", after: "192.175.49.0"},
		{prefix: "198.18.0.0/15", before: "198.17.255.255", after: "198.20.0.0"},
		{prefix: "198.51.100.0/24", before: "198.51.99.255", after: "198.51.101.0"},
		{prefix: "203.0.113.0/24", before: "203.0.112.255", after: "203.0.114.0"},
		{prefix: "224.0.0.0/4", before: "223.255.255.255"},
		{prefix: "240.0.0.0/4"},
	}

	policy := mustTestAddressPolicy(t, nil, []netip.Addr{})
	for _, test := range tests {
		t.Run(test.prefix, func(t *testing.T) {
			prefix := netip.MustParsePrefix(test.prefix)
			for _, boundary := range []netip.Addr{prefix.Addr(), prefixLastAddress(prefix)} {
				if !policy.prohibited(boundary) {
					t.Fatalf("boundary %s of %s was allowed", boundary, prefix)
				}
			}
			for _, neighbor := range []string{test.before, test.after} {
				if neighbor != "" && policy.prohibited(netip.MustParseAddr(neighbor)) {
					t.Fatalf("adjacent public address %s to %s was denied", neighbor, prefix)
				}
			}
		})
	}
}

func TestIPv6AllowBoundaryAndSpecialRangeBoundaries(t *testing.T) {
	policy := mustTestAddressPolicy(t, nil, []netip.Addr{})

	for _, address := range []string{
		"1fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		"4000::",
		"::",
		"::1",
		"fc00::1",
		"fe80::1",
		"ff02::1",
	} {
		if !policy.prohibited(netip.MustParseAddr(address)) {
			t.Fatalf("IPv6 address outside 2000::/3 was allowed: %s", address)
		}
	}
	for _, address := range []string{"2000::", "2001:4860:4860::8888", "3fff:1000::", "3fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
		if policy.prohibited(netip.MustParseAddr(address)) {
			t.Fatalf("public IPv6 address was denied: %s", address)
		}
	}

	tests := []struct {
		prefix string
		before string
		after  string
	}{
		{prefix: "2001::/23", before: "2000:ffff:ffff:ffff:ffff:ffff:ffff:ffff", after: "2001:200::"},
		{prefix: "2001:db8::/32", before: "2001:db7:ffff:ffff:ffff:ffff:ffff:ffff", after: "2001:db9::"},
		{prefix: "2002::/16", before: "2001:ffff:ffff:ffff:ffff:ffff:ffff:ffff", after: "2003::"},
		{prefix: "2620:4f:8000::/48", before: "2620:4f:7fff:ffff:ffff:ffff:ffff:ffff", after: "2620:4f:8001::"},
		{prefix: "3ffe::/16", before: "3ffd:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		{prefix: "3fff::/20", after: "3fff:1000::"},
		{prefix: "5f00::/16"},
	}
	for _, test := range tests {
		t.Run(test.prefix, func(t *testing.T) {
			prefix := netip.MustParsePrefix(test.prefix)
			for _, boundary := range []netip.Addr{prefix.Addr(), prefixLastAddress(prefix)} {
				if !policy.prohibited(boundary) {
					t.Fatalf("boundary %s of %s was allowed", boundary, prefix)
				}
			}
			for _, neighbor := range []string{test.before, test.after} {
				if neighbor != "" && policy.prohibited(netip.MustParseAddr(neighbor)) {
					t.Fatalf("adjacent public address %s to %s was denied", neighbor, prefix)
				}
			}
		})
	}
}

func TestMappedIPv6IsUnmappedBeforeClassification(t *testing.T) {
	policy := mustTestAddressPolicy(t, nil, []netip.Addr{})
	tests := []struct {
		address    string
		prohibited bool
	}{
		{address: "::ffff:127.0.0.1", prohibited: true},
		{address: "::ffff:10.1.2.3", prohibited: true},
		{address: "::ffff:192.0.2.1", prohibited: true},
		{address: "::ffff:93.184.216.34", prohibited: false},
	}
	for _, test := range tests {
		if got := policy.prohibited(netip.MustParseAddr(test.address)); got != test.prohibited {
			t.Fatalf("prohibited(%s) = %t, want %t", test.address, got, test.prohibited)
		}
	}
}

func TestExactLocalAndAdditionalCIDRDenials(t *testing.T) {
	policy := mustTestAddressPolicy(t, []string{"93.184.216.0/24", "2606:4700:4700::/48"}, []netip.Addr{
		netip.MustParseAddr("142.250.72.14"),
		netip.MustParseAddr("::ffff:203.0.114.8"),
	})
	for _, address := range []string{"93.184.216.34", "2606:4700:4700::1111", "142.250.72.14", "203.0.114.8"} {
		if !policy.prohibited(netip.MustParseAddr(address)) {
			t.Fatalf("configured address %s was allowed", address)
		}
	}
	for _, address := range []string{"93.184.217.1", "2606:4700:4701::1", "142.250.72.15", "203.0.114.9"} {
		if policy.prohibited(netip.MustParseAddr(address)) {
			t.Fatalf("unconfigured public address %s was denied", address)
		}
	}
}

func TestAdditionalCIDRsAreStrictlyValidated(t *testing.T) {
	tests := []string{
		"not-a-cidr",
		"93.184.216.1/24",
		"::ffff:192.0.2.0/120",
		"fe80::%en0/64",
	}
	for _, cidr := range tests {
		t.Run(cidr, func(t *testing.T) {
			_, err := newAddressPolicy([]string{cidr}, []netip.Addr{})
			assertReason(t, err, ReasonInvalidConfig)
		})
	}
	_, err := newAddressPolicy([]string{"93.184.216.0/24", "93.184.216.0/24"}, []netip.Addr{})
	assertReason(t, err, ReasonInvalidConfig)
}

func prefixLastAddress(prefix netip.Prefix) netip.Addr {
	prefix = prefix.Masked()
	if prefix.Addr().Is4() {
		bytes := prefix.Addr().As4()
		for bit := prefix.Bits(); bit < 32; bit++ {
			bytes[bit/8] |= 1 << (7 - uint(bit%8))
		}
		return netip.AddrFrom4(bytes)
	}
	bytes := prefix.Addr().As16()
	for bit := prefix.Bits(); bit < 128; bit++ {
		bytes[bit/8] |= 1 << (7 - uint(bit%8))
	}
	return netip.AddrFrom16(bytes)
}

func mustTestAddressPolicy(t *testing.T, additional []string, local []netip.Addr) addressPolicy {
	t.Helper()
	policy, err := newAddressPolicy(additional, local)
	if err != nil {
		t.Fatalf("newAddressPolicy failed: %v", err)
	}
	return policy
}
