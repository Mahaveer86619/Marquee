package metadata_test

import (
	"os"
	"testing"
	"time"

	"marquee/internal/httpx"
)

// TestMain shortens retry backoff so retry tests run quickly.
func TestMain(m *testing.M) {
	httpx.RetryBaseDelay = time.Millisecond
	os.Exit(m.Run())
}
