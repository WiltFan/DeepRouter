package testgate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// DenyPublicDial makes http.DefaultTransport refuse every host that is not
// loopback, and turns off its proxy. The gateway client clones that
// transport, so it inherits the refusal. httptest servers still answer.
// A refused dial is a network error, so a probe that reaches it fails.
// Tests call this from TestMain so a missed stub cannot reach a real API.
// The returned function restores the previous settings.
func DenyPublicDial() (restore func()) {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		panic("testgate: http.DefaultTransport is not *http.Transport")
	}
	prevDial, prevProxy := t.DialContext, t.Proxy
	var d net.Dialer
	t.Proxy = nil
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !loopbackAddr(addr) {
			return nil, fmt.Errorf("testgate: dial to %s denied in tests", addr)
		}
		return d.DialContext(ctx, network, addr)
	}
	t.CloseIdleConnections()
	return func() {
		t.DialContext, t.Proxy = prevDial, prevProxy
		t.CloseIdleConnections()
	}
}

func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
