package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestConsumeMaskedPrintsStarsAndBackspace(t *testing.T) {
	input := []byte{'s', 'k', 0x7f, 'a', 'b', 'c', '\n'}
	var echo bytes.Buffer
	got, err := consumeMasked(bytes.NewReader(input), &echo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "sabc" {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(echo.String(), "*") || strings.Contains(echo.String(), "sk") || strings.Contains(echo.String(), "abc") {
		t.Fatalf("echo %q", echo.String())
	}
	stars := strings.Count(echo.String(), "*")
	if stars != 5 {
		t.Fatalf("stars %d in %q", stars, echo.String())
	}
}
