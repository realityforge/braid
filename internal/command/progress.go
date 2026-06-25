package command

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const progressTickInterval = 5 * time.Second

type progressTicker interface {
	C() <-chan time.Time
	Stop()
}

type realProgressTicker struct {
	ticker *time.Ticker
}

func (t realProgressTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t realProgressTicker) Stop() {
	t.ticker.Stop()
}

type progressReporter struct {
	writer     io.Writer
	quiet      bool
	isTerminal func(io.Writer) bool
	newTicker  func(time.Duration) progressTicker
	interval   time.Duration
}

func newProgressReporter(writer io.Writer, quiet bool) progressReporter {
	return progressReporter{
		writer:     writer,
		quiet:      quiet,
		isTerminal: writerIsTerminal,
		newTicker: func(interval time.Duration) progressTicker {
			return realProgressTicker{ticker: time.NewTicker(interval)}
		},
		interval: progressTickInterval,
	}
}

func (r progressReporter) Start(message string) (*progressOperation, error) {
	op := &progressOperation{}
	if r.quiet || r.writer == nil {
		return op, nil
	}
	if r.isTerminal == nil {
		r.isTerminal = writerIsTerminal
	}
	if r.interval == 0 {
		r.interval = progressTickInterval
	}
	if r.newTicker == nil {
		r.newTicker = func(interval time.Duration) progressTicker {
			return realProgressTicker{ticker: time.NewTicker(interval)}
		}
	}

	op.writer = r.writer
	op.enabled = true
	op.terminal = r.isTerminal(r.writer)
	op.done = make(chan struct{})

	if op.terminal {
		if _, err := fmt.Fprint(r.writer, message); err != nil {
			return nil, err
		}
		op.lineOpen = true
		op.ticker = r.newTicker(r.interval)
		go op.runTicker()
		return op, nil
	}

	_, err := fmt.Fprintln(r.writer, message)
	return op, err
}

type progressOperation struct {
	writer   io.Writer
	enabled  bool
	terminal bool
	ticker   progressTicker
	done     chan struct{}
	once     sync.Once

	mu       sync.Mutex
	lineOpen bool
	err      error
}

func (op *progressOperation) Complete(message string) error {
	if op == nil || !op.enabled {
		return nil
	}
	op.stop()

	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.closeLineLocked(); err != nil {
		return err
	}
	if message == "" {
		return op.err
	}
	if _, err := fmt.Fprintln(op.writer, message); err != nil {
		return err
	}
	return op.err
}

func (op *progressOperation) Abort() error {
	if op == nil || !op.enabled {
		return nil
	}
	op.stop()

	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.closeLineLocked(); err != nil {
		return err
	}
	return op.err
}

func (op *progressOperation) runTicker() {
	for {
		select {
		case <-op.done:
			return
		case <-op.ticker.C():
			op.writeDot()
		}
	}
}

func (op *progressOperation) writeDot() {
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.err != nil {
		return
	}
	_, err := fmt.Fprint(op.writer, ".")
	if err != nil {
		op.err = err
		return
	}
	op.lineOpen = true
}

func (op *progressOperation) stop() {
	op.once.Do(func() {
		if op.done != nil {
			close(op.done)
		}
		if op.ticker != nil {
			op.ticker.Stop()
		}
	})
}

func (op *progressOperation) closeLineLocked() error {
	if !op.lineOpen {
		return op.err
	}
	op.lineOpen = false
	if _, err := fmt.Fprintln(op.writer); err != nil {
		return err
	}
	return op.err
}

func writerIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
