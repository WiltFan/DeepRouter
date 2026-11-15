package gateway

import (
	"os"
	"testing"

	"github.com/zfg5118/deeprouter/internal/testgate"
)

func TestMain(m *testing.M) {
	// Probes use a clone of http.DefaultTransport. Only loopback answers,
	// so a test cannot reach a real API.
	restore := testgate.DenyPublicDial()
	code := m.Run()
	restore()
	os.Exit(code)
}
