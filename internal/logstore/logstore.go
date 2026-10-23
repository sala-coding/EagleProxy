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
	"io"
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

	// wmu hands the queue over to the direct writer when the store is closed.
	// A write takes the read side and a close the write side: a line written
	// before the close goes through the queue and the writer drains it, and a
	// line written after the close is written to the file itself. Without the
	// handover the line written after the close sat in a queue nobody read any
	// more — it was dropped silently, and the last breadcrumb of a shutdown
	// never reached the file.
	wmu    sync.RWMutex
	closed bool
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
	go s.pruneOld()
	return s
}

// keepDays is how long a dated log file is kept. The dated categories get a
// fresh file every day (see Path), and nothing anywhere deleted the older ones:
// a machine that runs the app for years grew a log folder with no ceiling —
// one file per category per day, forever. Two weeks is well past "yesterday's
// problem" and short enough that the folder stays a thing a reader can look
// through. Files without a trailing -YYYY-MM-DD (tunserve.log and its kin) are
// not dated logs and are never touched here.
const keepDays = 14

// pruneOld deletes dated log files older than keepDays. Runs off the caller's
// path — a startup that would otherwise stat a folder it is about to write to —
// and takes the live directory when it runs, so a directory swapped in the
// meantime prunes its own files (SetDir kicks one too).
func (s *Store) pruneOld() {
	dir := s.Dir()
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -keepDays)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".log") {
			continue
		}
		base := strings.TrimSuffix(name, ".log")
		if len(base) < 11 { // <cat>-YYYY-MM-DD: at least one category char
			continue
		}
		day, err := time.Parse("2006-01-02", base[len(base)-10:])
		if err != nil {
			continue // not a dated file
		}
		if day.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// SetDir changes the output directory. Files opened for the previous directory
// are flushed and closed by the writer.
func (s *Store) SetDir(dir string) {
	s.dirMu.Lock()
	s.dir = dir
	s.dirMu.Unlock()
	// The new folder may carry files from before the reader moved back to it.
	go s.pruneOld()
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
	s.wmu.RLock()
	if s.closed {
		// The writer is gone; this line goes to the file itself.
		s.wmu.RUnlock()
		s.writeDirect(e)
		return
	}
	select {
	case s.ch <- e:
	default:
		s.dropped.Add(1)
	}
	s.wmu.RUnlock()
}

// writeDirect puts one line in the file itself, for the lines that arrive after the
// writer has stopped.
//
// The writer's own files are closed and flushed by the time it is gone, and the wait
// is for exactly that: the line then lands after the ones before it, instead of in
// the middle of them.
func (s *Store) writeDirect(e entry) {
	dir := s.Dir()
	if dir == "" {
		return
	}
	select {
	case <-s.stopped:
	case <-time.After(2 * time.Second):
	}
	if e.at.IsZero() {
		e.at = time.Now()
	}
	path := filepath.Join(dir, e.category+"-"+e.at.Format("2006-01-02")+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, e.line)
}

// Flush waits for everything queued to reach the disk. The writer would get
// there on its own within the flush interval, but a reader that comes right
// after a finished run would otherwise miss the last half second of it in the
// buffer — the run is over, and nothing is left to push. A stalled disk cannot
// hang the caller either: two seconds on the queue, two on the answer, then it
// gives up and the read goes ahead with what is already there.
func (s *Store) Flush() {
	// A closed store has no writer to wait for: the queue is drained and the
	// writer is gone, so the marker would never be answered and the wait would
	// be two seconds of nothing.
	s.wmu.RLock()
	closed := s.closed
	s.wmu.RUnlock()
	if closed {
		// A closed store has a writer still on its way down: it drains the
		// queue and flushes the buffers before it stops, and answering at once
		// — as this used to — let a reader os.ReadFile straight after a Flush
		// that had flushed nothing yet. Waiting on stopped IS the flush: it
		// closes after closeAll, the deferred final flush (see loop).
		select {
		case <-s.stopped:
		case <-time.After(2 * time.Second):
		}
		return
	}
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
	// The tail, read from the end: the panel asks for the last few hundred
	// lines, and a whole-day file was held in memory in full for them — every
	// open of the panel, and once a second while it is open.
	lines, err := readTailLines(s.Path(category, time.Now()), maxLines)
	if err != nil {
		return nil
	}
	return lines
}

// readTailLines returns a file's last maxLines whole lines, read from the end
// in growing chunks: a day-long log is not held in memory in full for the tail
// the panel shows. The chunk's first line is whole only when the byte before it
// is a newline (or the chunk is the file's beginning); a partial first line is
// the middle of a line an earlier chunk holds, and is dropped rather than
// shown cut in half. The file's trailing newline yields nothing: an absent
// last line is how "nothing logged" reads, and callers use len == 0 for it.
func readTailLines(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	const first = 256 << 10
	for chunk := int64(first); ; chunk *= 2 {
		if chunk >= size {
			data := make([]byte, size)
			if _, err := io.ReadFull(f, data); err != nil {
				return nil, err
			}
			return splitTailLines(data, maxLines, true), nil
		}
		// One byte before the chunk answers whether its first line arrived
		// whole; the read starts there and the body is what follows it.
		buf := make([]byte, chunk+1)
		if _, err := f.ReadAt(buf, size-chunk-1); err != nil && err != io.EOF {
			return nil, err
		}
		lines := splitTailLines(buf[1:], maxLines, buf[0] == '\n')
		if len(lines) >= maxLines {
			return lines, nil
		}
	}
}

// splitTailLines splits a chunk into at most the last maxLines whole lines.
// firstWhole says whether the chunk's first line arrived whole.
func splitTailLines(data []byte, maxLines int, firstWhole bool) []string {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if !firstWhole && len(lines) > 0 {
		lines = lines[1:]
	}
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
//
// The entry goes through the queue for that ordering, but it is not allowed to
// be dropped the way a plain line is: "latest run only" is a promise about what
// the reader will find, and a truncate the queue refused left the previous
// run's tail sitting in a file the next read presented as the latest run. The
// queue is flushed first, which leaves it empty enough that the send lands;
// only a queue that refilled in the microseconds between (writers that busy)
// falls back to the dropped counter's notice.
func (s *Store) Truncate(category string) {
	s.wmu.RLock()
	if s.closed {
		s.wmu.RUnlock()
		s.truncateDirect(category)
		return
	}
	s.wmu.RUnlock()
	e := entry{category: category, truncate: true, at: time.Now()}
	s.Flush()
	s.wmu.RLock()
	// The check is made AGAIN under this hold: Close may have landed in the
	// window the first check's release opened — and an entry sent past a
	// writer that has already drained and stopped is received by nobody. It
	// is not dropped (the counter never hears of it) and not fallen back
	// (nothing reads the queue again): a truncate that silently never
	// happened, which is exactly what this function exists to prevent.
	if s.closed {
		s.wmu.RUnlock()
		s.truncateDirect(category)
		return
	}
	defer s.wmu.RUnlock()
	select {
	case s.ch <- e:
	default:
		// The queue refilled between the flush and the send. One more flush;
		// if even that is refused, the dropped counter's notice is the honest
		// answer rather than a truncate that silently never happened.
		s.Flush()
		select {
		case s.ch <- e:
		default:
			s.dropped.Add(1)
		}
	}
}

// truncateDirect is Truncate after the writer is gone: the queue it used to be
// posted to has no reader, so the removal happened nowhere and silently — the
// speed-test log then grew across runs for the rest of the session. Same wait
// as writeDirect: the writer finishes its drain first, so this lands on the
// files it has closed rather than against an open handle.
func (s *Store) truncateDirect(category string) {
	select {
	case <-s.stopped:
	case <-time.After(2 * time.Second):
	}
	path := s.Path(category, time.Now())
	if err := os.Remove(path); err == nil {
		return
	}
	// Something holds the file (an editor the reader has it open in): emptying
	// it is the same result the caller asked for.
	if f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		_ = f.Close()
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
//
// A line written after the close is not lost: it is written to the file itself
// (see writeDirect). The shutdown path writes its last breadcrumb after closing
// the store, and that line is the one a reader looks for when a run stops
// halfway.
func (s *Store) Close() {
	s.once.Do(func() {
		s.wmu.Lock()
		s.closed = true
		close(s.stop)
		s.wmu.Unlock()
	})
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
			if err := os.Remove(path); err != nil {
				// Windows refuses to remove a file another handle has open
				// without FILE_SHARE_DELETE — and the store's own reader opens
				// the same path, as does any editor the reader has it open in.
				// The truncate was lost, the file was never emptied, and the
				// speed-test log grew with every run for good. Emptying it is
				// the same result the caller asked for.
				if f, ferr := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644); ferr == nil {
					_ = f.Close()
				}
			}
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
			// Drain first, then flush once, and only then answer the markers:
			// close(done) used to happen here while the lines were still in
			// the bufio buffers — the actual flush is the deferred closeAll
			// that runs after this return — so a Flush() racing the shutdown
			// reported "on disk" over a file whose tail was not, and the
			// shutdown breadcrumb the reader looks for was the piece missing.
			var markers []chan struct{}
			for {
				select {
				case e := <-s.ch:
					if e.flush {
						markers = append(markers, e.done)
						continue
					}
					write(e)
					continue
				default:
				}
				break
			}
			reportDropped("core")
			flush()
			for _, done := range markers {
				close(done)
			}
			return
		}
	}
}
