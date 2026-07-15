package command

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fakeProgressTicker struct {
	ch      chan time.Time
	stopped bool
}

func newFakeProgressTicker() *fakeProgressTicker {
	return &fakeProgressTicker{ch: make(chan time.Time)}
}

func (t *fakeProgressTicker) C() <-chan time.Time {
	return t.ch
}

func (t *fakeProgressTicker) Stop() {
	t.stopped = true
}

func TestProgressReporterWritesLineBasedNonTTYProgress(t *testing.T) {
	var out lockedBuffer
	reporter := newProgressReporter(&out, false)

	op, err := reporter.Start("Braid: fetching mirror vendor/basic")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := op.Complete("Braid: fetched mirror vendor/basic"); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	got := out.String()
	want := "Braid: fetching mirror vendor/basic\nBraid: fetched mirror vendor/basic\n"
	if got != want {
		t.Fatalf("progress output = %q, want %q", got, want)
	}
}

func TestProgressReporterQuietSuppressesProgress(t *testing.T) {
	var out lockedBuffer
	reporter := newProgressReporter(&out, true)

	op, err := reporter.Start("Braid: fetching mirror vendor/basic")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := op.Complete("Braid: fetched mirror vendor/basic"); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if got := out.String(); got != "" {
		t.Fatalf("quiet progress output = %q, want empty", got)
	}
}

func TestProgressReporterTTYDotsAndCompletion(t *testing.T) {
	var out lockedBuffer
	ticker := newFakeProgressTicker()
	reporter := newProgressReporter(&out, false)
	reporter.isTerminal = func(io.Writer) bool { return true }
	reporter.newTicker = func(time.Duration) progressTicker { return ticker }

	op, err := reporter.Start("Braid: fetching mirror vendor/basic")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	ticker.ch <- time.Now()
	ticker.ch <- time.Now()
	waitForProgressOutput(t, &out, "..")
	if err := op.Complete("Braid: fetched mirror vendor/basic"); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if !ticker.stopped {
		t.Fatal("ticker was not stopped")
	}
	got := out.String()
	want := "Braid: fetching mirror vendor/basic..\nBraid: fetched mirror vendor/basic\n"
	if got != want {
		t.Fatalf("progress output = %q, want %q", got, want)
	}
}

func TestProgressReporterPausesAndResumesTTYDots(t *testing.T) {
	var out lockedBuffer
	ticker := newFakeProgressTicker()
	reporter := newProgressReporter(&out, false)
	reporter.isTerminal = func(io.Writer) bool { return true }
	reporter.newTicker = func(time.Duration) progressTicker { return ticker }

	op, err := reporter.Start("Braid: pushing mirror vendor/basic")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	op.pause()
	op.writeDot()
	if got, want := out.String(), "Braid: pushing mirror vendor/basic"; got != want {
		t.Fatalf("paused progress output = %q, want %q", got, want)
	}
	op.resume()
	ticker.ch <- time.Now()
	waitForProgressOutput(t, &out, ".")
	if err := op.Complete("Braid: pushed mirror vendor/basic"); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	got := out.String()
	want := "Braid: pushing mirror vendor/basic.\nBraid: pushed mirror vendor/basic\n"
	if got != want {
		t.Fatalf("progress output = %q, want %q", got, want)
	}
}

func TestProgressSeparatedWriterBreaksOpenTTYLine(t *testing.T) {
	var out lockedBuffer
	ticker := newFakeProgressTicker()
	reporter := newProgressReporter(&out, false)
	reporter.isTerminal = func(io.Writer) bool { return true }
	reporter.newTicker = func(time.Duration) progressTicker { return ticker }

	op, err := reporter.Start("Braid: pushing mirror vendor/basic")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	separated := newProgressSeparatedWriter(op, &out)
	if _, err := separated.Write([]byte("Braid: generating push commit message for vendor/basic using external tool\n")); err != nil {
		t.Fatalf("separated Write returned error: %v", err)
	}
	ticker.ch <- time.Now()
	waitForProgressOutput(t, &out, "using external tool\n.")
	if err := op.Complete("Braid: pushed mirror vendor/basic"); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	got := out.String()
	want := "Braid: pushing mirror vendor/basic\nBraid: generating push commit message for vendor/basic using external tool\n.\nBraid: pushed mirror vendor/basic\n"
	if got != want {
		t.Fatalf("progress output = %q, want %q", got, want)
	}
}

func TestProgressSeparatedWriterLeavesNonTTYProgressUnchanged(t *testing.T) {
	var out lockedBuffer
	reporter := newProgressReporter(&out, false)

	op, err := reporter.Start("Braid: pushing mirror vendor/basic")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	separated := newProgressSeparatedWriter(op, &out)
	if _, err := separated.Write([]byte("Braid: generating push commit message for vendor/basic using external tool\n")); err != nil {
		t.Fatalf("separated Write returned error: %v", err)
	}
	if err := op.Complete("Braid: pushed mirror vendor/basic"); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	got := out.String()
	want := "Braid: pushing mirror vendor/basic\nBraid: generating push commit message for vendor/basic using external tool\nBraid: pushed mirror vendor/basic\n"
	if got != want {
		t.Fatalf("progress output = %q, want %q", got, want)
	}
}

func TestProgressReporterAbortCleansUpOpenTTYLine(t *testing.T) {
	var out lockedBuffer
	ticker := newFakeProgressTicker()
	reporter := newProgressReporter(&out, false)
	reporter.isTerminal = func(io.Writer) bool { return true }
	reporter.newTicker = func(time.Duration) progressTicker { return ticker }

	op, err := reporter.Start("Braid: fetching mirror vendor/basic")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	ticker.ch <- time.Now()
	waitForProgressOutput(t, &out, ".")
	if err := op.Abort(); err != nil {
		t.Fatalf("Abort returned error: %v", err)
	}

	if !ticker.stopped {
		t.Fatal("ticker was not stopped")
	}
	got := out.String()
	want := "Braid: fetching mirror vendor/basic.\n"
	if got != want {
		t.Fatalf("progress output = %q, want %q", got, want)
	}
}

func waitForProgressOutput(t *testing.T, out *lockedBuffer, want string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if strings.Contains(out.String(), want) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("progress output = %q, want substring %q", out.String(), want)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
