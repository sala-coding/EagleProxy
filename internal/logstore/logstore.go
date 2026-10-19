// Package logstore appends categorised text logs to disk.
//
// Layout: <dir>/<category>-YYYY-MM-DD.log, one file per category per day.
//
// Writing is asynchronous: producers (including the core's stdout copier) only
// push onto a buffered queue, while a single goroutine owns the file handles
// and flushes them periodically. A producer therefore never blocks on disk I/O.
package logstore

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	queueSize  = 4096
	flushEvery = 500 * time.Millisecond
	writeBuf   = 16 * 1024

	// tsLayout is the timestamp every stored line carries: to the millisecond,
	// because a stream of several sources is ordered by it.
	tsLayout = "15:04:05.000"
)

// Stamp returns a line as it is stored: the timestamp, a space, and the text.
//
// It is exported because one line is read in two places at once — the file, and the
// panel's live stream or the core's in-memory buffer — and the two readings have to
// carry the same time. The timestamp used to exist only inside the file writer,
// which is exactly how the panel came to show a line timed after a reload and
// untimed while it streamed.
func Stamp(line string) string {
	return time.Now().Format(tsLayout) + " " + line
}

// HasStamp reports whether a line already carries that timestamp, so a line that
// was stamped where it was produced is not given a second one on the way to disk.
// The shape is checked against "00:00:00.000 " — the digits are digits, the
// punctuation is where the layout puts it — rather than against a parsed time,
// because the question is only whether this line already has a timestamp on it.
func HasStamp(line string) bool {
	const shape = "00:00:00.000 "
	if len(line) < len(shape) {
		return false
	}
	for i := 0; i < len(shape); i++ {
		if shape[i] == '0' {
			if line[i] < '0' || line[i] > '9' {
				return false
			}
			continue
		}
		if line[i] != shape[i] {
			return false
		}
	}
	return true
}

type entry struct {
	category string
	line     string
	at       time.Time
	truncate bool
	// flush is the marker a reader sends when it is about to read a file from
	// disk: the writer answers it once every line queued before the marker is
	// on disk. done is then closed, and the reader continues.
	flush bool
	done  chan struct{}
}

type openFile struct {
	dir string
	f   *os.File
	w   *bufio.Writer
}

// Store appends lines to daily per-category log files.
type Store struct {
	dirMu sync.RWMutex
	dir   string

	ch      chan entry
	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once
	dropped atomic.Int64
}

// New creates a store rooted at dir and starts its writer goroutine.
func New(dir string) *Store {
	s := &Store{
		dir:     dir,
		ch:      make(chan entry, queueSize),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go s.loop()
	return s
}

// SetDir changes the output directory. Files opened for the previous directory
// are flushed and closed by the writer.
func (s *Store) SetDir(dir string) {
	s.dirMu.Lock()
	s.dir = dir
	s.dirMu.Unlock()
}

// Dir returns the output directory.
func (s *Store) Dir() string {
	s.dirMu.RLock()
	defer s.dirMu.RUnlock()
	return s.dir
}

// Path returns the file a category writes to on the given day.
func (s *Store) Path(category string, day time.Time) string {
	return filepath.Join(s.Dir(), fmt.Sprintf("%s-%s.log", category, day.Format("2006-01-02")))
}

// Write queues one line, stamping it if it does not already carry a timestamp.
// It never blocks: when the queue is full (disk stalled) the line is dropped and a
// marker is written later.
//
// Accepting either form is what lets a line be stamped once, where it is produced,
// and then written here unchanged — the panel's live stream and this file showing
// the same text for the same line (see Stamp).
func (s *Store) Write(category, line string) {
	// One instant for both the stamp and the day the line is filed under: read
	// apart, a line written across midnight could be stamped into the next day and
	// filed under the last one.
	at := time.Now()
	if !HasStamp(line) {
		line = at.Format(tsLayout) + " " + line
	}
	e := entry{category: category, line: line, at: at}
	select {
	case s.ch <- e:
	default:
		s.dropped.Add(1)
	}
}

// Flush waits for everything queued to reach the disk. The writer would get
// there on its own within the flush interval, but a reader that comes right
// after a finished run would otherwise miss the last half second of it in the
// buffer — the run is over, and nothing is left to push. A stalled disk cannot
// hang the caller either: two seconds on the queue, two on the answer, then it
// gives up and the read goes ahead with what is already there.
func (s *Store) Flush() {
	done := make(chan struct{})
	select {
	case s.ch <- entry{flush: true, done: done}:
	case <-time.After(2 * time.Second):
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// Categories returns every category that already has a file for the current
// day, sorted. The log panel uses it to offer a switcher, so only categories
// that actually recorded something show up.
func (s *Store) Categories() []string {
	dir := s.Dir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	suffix := "-" + time.Now().Format("2006-01-02") + ".log"
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, suffix) {
			continue
		}
		if cat := strings.TrimSuffix(name, suffix); cat != "" {
			out = append(out, cat)
		}
	}
	sort.Strings(out)
	return out
}

// Read returns up to maxLines trailing lines of a category's file for the
// current day. The speed-test categories stream their lines to the UI as they
// are produced, but the file is what the panel re-reads at the end of a run
// and on every open, so this is what has to be complete the moment it is asked.
func (s *Store) Read(category string, maxLines int) []string {
	if category == "" || maxLines <= 0 {
		return nil
	}
	// Straight from disk is the only way to show the speed-test categories, so
	// what is still sitting in the writer's queue has to be put there first:
	// the panel re-reads the moment a run ends, and the tail of that run would
	// otherwise appear only on the read after it (see Flush).
	s.Flush()
	data, err := os.ReadFile(s.Path(category, time.Now()))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	// a trailing newline yields one empty element; drop it so callers can use
	// len(lines) == 0 to mean "nothing logged"
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

// Truncate discards the current contents of a category's file, so the block
// written next starts from an empty file. Use it for "latest run only" logs, as
// opposed to the default append-only behaviour. The removal happens on the
// writer goroutine, so it stays ordered with respect to queued lines.
func (s *Store) Truncate(category string) {
	select {
	case s.ch <- entry{category: category, truncate: true, at: time.Now()}:
	default:
		s.dropped.Add(1)
	}
}

// Section writes a titled heading, marking the start of a new block.
//
// It used to draw a bar of "=" on both sides of the title. In a plain text file
// that reads as a divider, but the log panel shows four sources in one stream,
// and there a full-width bar of punctuation reads as a line of shouted output
// rather than as a heading — which is worse than no heading at all. "##" is a
// heading in every other reader too, so the file is no less clear for it.
func (s *Store) Section(category, title string) {
	s.Write(category, "## "+title)
}

// Close flushes everything still queued and stops the writer. It gives up after
// two seconds so a stalled disk can never hang the shutdown.
func (s *Store) Close() {
	s.once.Do(func() { close(s.stop) })
	select {
	case <-s.stopped:
	case <-time.After(2 * time.Second):
	}
}

func (s *Store) loop() {
	defer close(s.stopped)

	files := map[string]*openFile{}
	closeAll := func() {
		for _, o := range files {
			_ = o.w.Flush()
			_ = o.f.Close()
		}
		files = map[string]*openFile{}
	}
	defer closeAll()

	// flush writes buffered lines out and then closes every handle, so the
	// files are never locked between operations: on Windows an open handle
	// blocks delete/rename by other programs, and a desktop app should not sit
	// on the user's log files all day. The next write simply reopens the file
	// (O_APPEND), which is cheap for the handful of lines a desktop app writes.
	flush := func() {
		for key, o := range files {
			_ = o.w.Flush()
			_ = o.f.Close()
			delete(files, key)
		}
	}

	write := func(e entry) {
		dir := s.Dir()
		if dir == "" {
			return
		}
		if e.at.IsZero() {
			e.at = time.Now()
		}
		day := e.at.Format("2006-01-02")
		key := dir + "|" + e.category + "-" + day
		path := filepath.Join(dir, e.category+"-"+day+".log")

		if e.truncate {
			if o := files[key]; o != nil {
				_ = o.w.Flush()
				_ = o.f.Close()
				delete(files, key)
			}
			_ = os.Remove(path)
			return
		}

		o := files[key]
		if o == nil {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return
			}
			o = &openFile{dir: dir, f: f, w: bufio.NewWriterSize(f, writeBuf)}
			files[key] = o
		}
		// The line arrives already stamped — Write stamps what has none — so this
		// is a write of the text as the panel saw it, not a second timestamping.
		_, _ = fmt.Fprintln(o.w, e.line)
	}

	reportDropped := func(category string) {
		if n := s.dropped.Swap(0); n > 0 {
			write(entry{category: category, at: time.Now(),
				line: Stamp(fmt.Sprintf("... 写入积压，已丢弃 %d 行日志", n))})
		}
	}

	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	for {
		select {
		case e := <-s.ch:
			if e.flush {
				flush()
				close(e.done)
				continue
			}
			write(e)
			if s.dropped.Load() > 0 {
				reportDropped(e.category)
			}
		case <-ticker.C:
			reportDropped("core")
			flush()
		case <-s.stop:
			for {
				select {
				case e := <-s.ch:
					if e.flush {
						// closeAll is the flush this marker was waiting on; the
						// reader only needs to hear that the wait is over.
						close(e.done)
						continue
					}
					write(e)
					continue
				default:
				}
				break
			}
			reportDropped("core")
			return
		}
	}
}
