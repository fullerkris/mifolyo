package securefetch

import (
	"net/netip"
)

var prohibitedIPv4Prefixes = mustPrefixes(
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.31.196.0/24",
	"192.52.193.0/24",
	"192.88.99.0/24",
	"192.168.0.0/16",
	"192.175.48.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
)

var (
	publicIPv6Prefix       = netip.MustParsePrefix("2000::/3")
	prohibitedIPv6Prefixes = mustPrefixes(
		"2001::/23",
		"2001:db8::/32",
		"2002::/16",
		"2620:4f:8000::/48",
		"3ffe::/16",
		"3fff::/20",
		"5f00::/16",
	)
)

type addressPolicy struct {
	additional []netip.Prefix
	local      map[netip.Addr]struct{}
}

func newAddressPolicy(additionalCIDRs []string, localAddresses []netip.Addr) (addressPolicy, error) {
	policy := addressPolicy{
		additional: make([]netip.Prefix, 0, len(additionalCIDRs)),
		local:      make(map[netip.Addr]struct{}, len(localAddresses)),
	}

	seenPrefixes := make(map[netip.Prefix]struct{}, len(additionalCIDRs))
	for _, rawPrefix := range additionalCIDRs {
		prefix, err := netip.ParsePrefix(rawPrefix)
		if err != nil || prefix != prefix.Masked() || prefix.Addr().Zone() != "" || prefix.Addr().Is4In6() {
			return addressPolicy{}, newError(ReasonInvalidConfig, "validate_additional_cidr", err)
		}
		if _, duplicate := seenPrefixes[prefix]; duplicate {
			return addressPolicy{}, newError(ReasonInvalidConfig, "validate_additional_cidr", nil)
		}
		seenPrefixes[prefix] = struct{}{}
		policy.additional = append(policy.additional, prefix)
	}

	for _, address := range localAddresses {
		if !address.IsValid() || address.Zone() != "" {
			return addressPolicy{}, newError(ReasonInvalidConfig, "validate_local_address", nil)
		}
		policy.local[address.Unmap()] = struct{}{}
	}
	return policy, nil
}

func (p addressPolicy) prohibited(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return true
	}
	address = address.Unmap()
	if _, isLocal := p.local[address]; isLocal {
		return true
	}
	for _, prefix := range p.additional {
		if prefix.Contains(address) {
			return true
		}
	}

	if address.Is4() {
		return prefixContains(prohibitedIPv4Prefixes, address)
	}
	if !address.Is6() || !publicIPv6Prefix.Contains(address) {
		return true
	}
	return prefixContains(prohibitedIPv6Prefixes, address)
}

func prefixContains(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func mustPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, len(values))
	for index, value := range values {
		prefixes[index] = netip.MustParsePrefix(value)
	}
	return prefixes
}
