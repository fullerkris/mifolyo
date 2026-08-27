package securefetch

import (
	"net"
	"net/netip"
	"os"
	"strings"
	"time"
)

var proxyEnvironmentNames = [...]string{
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"ALL_PROXY",
	"http_proxy",
	"https_proxy",
	"all_proxy",
}

// ValidateNoProxyEnvironment fails when a proxy variable is nonempty. It never
// includes the variable's value in the returned error. Fetcher ignores proxy
// variables regardless, but New also calls this function so a misconfigured
// deployment fails visibly instead of appearing to honor a proxy.
func ValidateNoProxyEnvironment() error {
	for _, name := range proxyEnvironmentNames {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			return newError(ReasonProxyEnvironment, "validate_proxy_environment", nil)
		}
	}
	return nil
}

// New constructs a direct-only Fetcher. It snapshots local interfaces and all
// configuration; it does not make DNS queries or outbound connections.
func New(config Config) (*Fetcher, error) {
	if err := ValidateNoProxyEnvironment(); err != nil {
		return nil, err
	}

	dnsTimeout, err := boundedDuration(config.DNSLookupTimeout, defaultDNSLookupTimeout, maximumPhaseTimeout)
	if err != nil {
		return nil, err
	}
	dialTimeout, err := boundedDuration(config.DialTimeout, defaultDialTimeout, maximumPhaseTimeout)
	if err != nil {
		return nil, err
	}
	tlsTimeout, err := boundedDuration(config.TLSHandshakeTimeout, defaultTLSHandshakeTimeout, maximumPhaseTimeout)
	if err != nil {
		return nil, err
	}
	headerTimeout, err := boundedDuration(config.ResponseHeaderTimeout, defaultResponseHeaderTimeout, maximumPhaseTimeout)
	if err != nil {
		return nil, err
	}
	totalTimeout, err := boundedDuration(config.TotalTimeout, defaultTotalTimeout, maximumTotalTimeout)
	if err != nil {
		return nil, err
	}
	if dnsTimeout > totalTimeout || dialTimeout > totalTimeout || tlsTimeout > totalTimeout || headerTimeout > totalTimeout {
		return nil, newError(ReasonInvalidConfig, "validate_timeouts", nil)
	}

	maxHeaders := config.MaxResponseHeaderBytes
	if maxHeaders == 0 {
		maxHeaders = defaultMaxResponseHeader
	}
	if maxHeaders < 0 || maxHeaders > maximumResponseHeaders {
		return nil, newError(ReasonInvalidConfig, "validate_response_header_limit", nil)
	}

	localAddresses := config.LocalAddresses
	if localAddresses == nil {
		localAddresses, err = interfaceAddresses()
		if err != nil {
			return nil, newError(ReasonInvalidConfig, "snapshot_local_interfaces", err)
		}
	}
	addresses, err := newAddressPolicy(config.AdditionalDeniedCIDRs, localAddresses)
	if err != nil {
		return nil, err
	}

	resolver := config.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := config.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}

	var rootCAs = config.RootCAs
	if rootCAs != nil {
		rootCAs = rootCAs.Clone()
	}

	return &Fetcher{
		resolver:               resolver,
		dialer:                 dialer,
		rootCAs:                rootCAs,
		dnsLookupTimeout:       dnsTimeout,
		dialTimeout:            dialTimeout,
		tlsHandshakeTimeout:    tlsTimeout,
		responseHeaderTimeout:  headerTimeout,
		totalTimeout:           totalTimeout,
		maxResponseHeaderBytes: maxHeaders,
		addresses:              addresses,
	}, nil
}

func boundedDuration(value, fallback, maximum time.Duration) (time.Duration, error) {
	if value == 0 {
		value = fallback
	}
	if value < 0 || value > maximum {
		return 0, newError(ReasonInvalidConfig, "validate_timeout", nil)
	}
	return value, nil
}

func interfaceAddresses() ([]netip.Addr, error) {
	interfaceAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	addresses := make([]netip.Addr, 0, len(interfaceAddrs))
	for _, interfaceAddr := range interfaceAddrs {
		value := interfaceAddr.String()
		if slash := strings.LastIndexByte(value, '/'); slash >= 0 {
			value = value[:slash]
		}
		if zone := strings.LastIndexByte(value, '%'); zone >= 0 {
			value = value[:zone]
		}
		address, parseErr := netip.ParseAddr(value)
		if parseErr != nil {
			return nil, parseErr
		}
		addresses = append(addresses, address.Unmap())
	}
	return addresses, nil
}
