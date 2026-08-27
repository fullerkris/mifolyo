package securefetch

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"time"
)

type pinnedDialer struct {
	dialer            Dialer
	expectedAuthority string
	port              uint16
	addresses         []netip.Addr
	timeout           time.Duration
}

func (d pinnedDialer) dialContext(ctx context.Context, network, authority string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, newError(ReasonDialNetwork, "dial", nil)
	}
	if authority != d.expectedAuthority {
		return nil, newError(ReasonDialAuthority, "dial", nil)
	}
	if ctx == nil {
		return nil, newError(ReasonInvalidArgument, "dial", nil)
	}

	dialContext, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var lastError error
	tried := false
	for _, address := range d.addresses {
		if network == "tcp4" && !address.Is4() || network == "tcp6" && !address.Is6() {
			continue
		}
		tried = true
		if err := dialContext.Err(); err != nil {
			lastError = err
			break
		}

		numericAuthority := net.JoinHostPort(address.String(), strconv.Itoa(int(d.port)))
		connection, err := d.dialer.DialContext(dialContext, network, numericAuthority)
		if err != nil {
			lastError = err
			continue
		}
		if err := verifyRemoteAddress(connection, address, d.port); err != nil {
			_ = connection.Close()
			return nil, err
		}
		return connection, nil
	}
	if !tried && lastError == nil {
		lastError = net.UnknownNetworkError(network)
	}
	return nil, newError(ReasonDialFailed, "dial", lastError)
}

func verifyRemoteAddress(connection net.Conn, selected netip.Addr, port uint16) error {
	if connection == nil || connection.RemoteAddr() == nil {
		return newError(ReasonRemoteAddressMismatch, "verify_remote_address", nil)
	}

	remote, err := remoteAddrPort(connection.RemoteAddr())
	if err != nil || remote.Addr().Zone() != "" || remote.Addr().Unmap() != selected.Unmap() || remote.Port() != port {
		return newError(ReasonRemoteAddressMismatch, "verify_remote_address", err)
	}
	return nil
}

func remoteAddrPort(address net.Addr) (netip.AddrPort, error) {
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		result := tcpAddress.AddrPort()
		if !result.IsValid() {
			return netip.AddrPort{}, net.InvalidAddrError("invalid TCP remote address")
		}
		return result, nil
	}
	return netip.ParseAddrPort(address.String())
}
