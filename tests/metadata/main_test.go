package metadata_test

import (
	"os"
	"testing"
	"time"

	"marquee/internal/metadata"
)

// TestMain shortens retry backoff so retry tests run quickly.
func TestMain(m *testing.M) {
	metadata.RetryBaseDelay = time.Millisecond
	os.Exit(m.Run())
}
