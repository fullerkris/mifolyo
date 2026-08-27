package securefetch

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateNoProxyEnvironmentRejectsEverySupportedVariableWithoutLeakingValue(t *testing.T) {
	clearProxyEnvironment(t)
	if err := ValidateNoProxyEnvironment(); err != nil {
		t.Fatalf("empty proxy environment was rejected: %v", err)
	}

	for _, name := range proxyEnvironmentNames {
		t.Run(name, func(t *testing.T) {
			clearProxyEnvironment(t)
			secret := "http://user:very-secret@example.invalid/?token=hidden"
			t.Setenv(name, secret)

			err := ValidateNoProxyEnvironment()
			assertReason(t, err, ReasonProxyEnvironment)
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "very-secret") || strings.Contains(err.Error(), "hidden") {
				t.Fatalf("proxy value leaked in error: %q", err)
			}

			_, err = New(Config{LocalAddresses: []netip.Addr{}})
			assertReason(t, err, ReasonProxyEnvironment)
		})
	}
}

func TestNewRejectsUnboundedConfiguration(t *testing.T) {
	clearProxyEnvironment(t)
	tests := []Config{
		{LocalAddresses: []netip.Addr{}, DNSLookupTimeout: -time.Second},
		{LocalAddresses: []netip.Addr{}, DialTimeout: maximumPhaseTimeout + time.Nanosecond},
		{LocalAddresses: []netip.Addr{}, TotalTimeout: maximumTotalTimeout + time.Nanosecond},
		{LocalAddresses: []netip.Addr{}, DNSLookupTimeout: 2 * time.Second, TotalTimeout: time.Second},
		{LocalAddresses: []netip.Addr{}, MaxResponseHeaderBytes: -1},
		{LocalAddresses: []netip.Addr{}, MaxResponseHeaderBytes: maximumResponseHeaders + 1},
	}
	for index, config := range tests {
		if _, err := New(config); ReasonOf(err) != ReasonInvalidConfig {
			t.Fatalf("config %d error = %v, reason %q", index, err, ReasonOf(err))
		}
	}
}

func TestPinnedDialerRejectsUnexpectedNetworkAndAuthorityBeforeDial(t *testing.T) {
	var calls atomic.Int32
	underlying := dialerFunc(func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("must not dial")
	})
	pinned := pinnedDialer{
		dialer:            underlying,
		expectedAuthority: "example.com:80",
		port:              80,
		addresses:         []netip.Addr{netip.MustParseAddr("93.184.216.34")},
		timeout:           time.Second,
	}

	_, err := pinned.dialContext(context.Background(), "udp", "example.com:80")
	assertReason(t, err, ReasonDialNetwork)
	_, err = pinned.dialContext(context.Background(), "tcp", "attacker.example:80")
	assertReason(t, err, ReasonDialAuthority)
	if calls.Load() != 0 {
		t.Fatalf("underlying dialer was called %d times", calls.Load())
	}
}

func TestPinnedDialerClosesAndRejectsRemoteAddressMismatch(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	wrapped := &trackedRemoteConn{
		Conn:   client,
		remote: net.TCPAddrFromAddrPort(netip.MustParseAddrPort("142.250.72.14:80")),
	}
	underlying := dialerFunc(func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "93.184.216.34:80" {
			t.Fatalf("unexpected underlying dial: %q %q", network, address)
		}
		return wrapped, nil
	})
	pinned := pinnedDialer{
		dialer:            underlying,
		expectedAuthority: "example.com:80",
		port:              80,
		addresses:         []netip.Addr{netip.MustParseAddr("93.184.216.34")},
		timeout:           time.Second,
	}

	_, err := pinned.dialContext(context.Background(), "tcp", "example.com:80")
	assertReason(t, err, ReasonRemoteAddressMismatch)
	if !wrapped.closed.Load() {
		t.Fatal("mismatched connection was not closed")
	}
}

func TestPinnedDialerAcceptsMappedRemoteForSelectedIPv4(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	wrapped := &trackedRemoteConn{
		Conn:   client,
		remote: stringAddr{network: "tcp", value: "[::ffff:93.184.216.34]:443"},
	}
	pinned := pinnedDialer{
		dialer: dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			return wrapped, nil
		}),
		expectedAuthority: "example.com:443",
		port:              443,
		addresses:         []netip.Addr{netip.MustParseAddr("93.184.216.34")},
		timeout:           time.Second,
	}
	connection, err := pinned.dialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("mapped remote was rejected: %v", err)
	}
	_ = connection.Close()
}

func TestTransportIsDirectHTTP1OnlyAndVerifiesTLS(t *testing.T) {
	clearProxyEnvironment(t)
	fetcher, err := New(Config{LocalAddresses: []netip.Addr{}})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	target := fetchTarget{
		host:      "example.com",
		port:      443,
		authority: "example.com:443",
	}
	transport := fetcher.transportFor(target, []netip.Addr{netip.MustParseAddr("93.184.216.34")})
	defer transport.CloseIdleConnections()

	if transport.Proxy != nil {
		t.Fatal("transport has a proxy callback")
	}
	if transport.DialTLS != nil || transport.DialTLSContext != nil {
		t.Fatal("transport has a TLS dial path that could bypass pinned DialContext")
	}
	if !transport.DisableKeepAlives || !transport.DisableCompression || transport.ForceAttemptHTTP2 {
		t.Fatalf("transport hardening flags are not set: %#v", transport)
	}
	if transport.TLSNextProto == nil || len(transport.TLSNextProto) != 0 {
		t.Fatal("HTTP/2 TLS protocol map is not explicitly empty")
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS certificate verification is disabled")
	}
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 || transport.TLSClientConfig.ServerName != "example.com" {
		t.Fatalf("unexpected TLS policy: %#v", transport.TLSClientConfig)
	}
	if len(transport.TLSClientConfig.NextProtos) != 1 || transport.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Fatalf("unexpected ALPN protocols: %v", transport.TLSClientConfig.NextProtos)
	}
}

func clearProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range proxyEnvironmentNames {
		t.Setenv(name, "")
	}
}

func assertReason(t *testing.T, err error, want Reason) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected reason %q, got nil", want)
	}
	if got := ReasonOf(err); got != want {
		t.Fatalf("reason = %q (%T %v), want %q", got, err, err, want)
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Code() != string(want) {
		t.Fatalf("error is not typed with code %q: %T %v", want, err, err)
	}
}

type dialerFunc func(context.Context, string, string) (net.Conn, error)

func (function dialerFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return function(ctx, network, address)
}

type trackedRemoteConn struct {
	net.Conn
	remote net.Addr
	closed atomic.Bool
}

func (connection *trackedRemoteConn) RemoteAddr() net.Addr {
	return connection.remote
}

func (connection *trackedRemoteConn) Close() error {
	connection.closed.Store(true)
	return connection.Conn.Close()
}

type stringAddr struct {
	network string
	value   string
}

func (address stringAddr) Network() string { return address.network }
func (address stringAddr) String() string  { return address.value }
