// Package wsrecord пишет сырой поток рыночных данных WS БКС на диск — как
// пришёл, до разбора, с временем получения на нашей стороне. Нужен для двух
// вещей, которых по M5-истории не сделать: проверить модель исполнения лимита
// по тикам (docs/analysis/0004-live-decides-on-forming-bar.md, п. 2) и прогнать
// записанный день через live-код, сверив с тем, что бот сделал на самом деле.
//
// Формат: <dir>/<YYYY-MM-DD по МСК>.jsonl.gz, строка на сообщение:
//
//	{"recv":"2026-09-23T07:00:01.123456789Z","msg":{...сообщение как есть...}}
//
// Рестарт в тот же день дописывает в файл новый gzip-член — многочленный gzip
// читается штатно (zcat, Python gzip). Сообщение, не являющееся JSON, пишется
// строкой в поле "raw". Маркеры сессии WS (переподключения) приходят как
// обычные сообщения с responseType "_session" — по ним видны дыры в потоке.
//
// Запись не имеет права тормозить торговлю: Record не блокируется, при
// переполнении буфера сообщение отбрасывается и считается в dropped.
package wsrecord

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"bcs-trading-bot/internal/logx"
)

const (
	// bufferSize — сколько сообщений может ждать записи. Пик потока на
	// открытии — сотни сообщений в секунду; запаса хватает на десятки секунд
	// медленного диска.
	bufferSize = 16384
	// flushEvery — как часто сбрасывать gzip на диск. При падении процесса
	// теряется не больше этого интервала.
	flushEvery = 5 * time.Second
)

type message struct {
	recv time.Time
	raw  []byte
}

// Recorder — фоновый писатель потока. Создаётся в composition root, живёт
// весь прогон, закрывается через Close.
type Recorder struct {
	dir string
	loc *time.Location

	in   chan message
	done chan struct{}
	wg   sync.WaitGroup

	dropped atomic.Int64
}

// New создаёт каталог и запускает писателя. День файла считается в loc
// (торговый день МСК), чтобы ночная граница UTC не резала сессию пополам.
func New(dir string, loc *time.Location) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("wsrecord: каталог %s: %w", dir, err)
	}
	r := &Recorder{
		dir:  dir,
		loc:  loc,
		in:   make(chan message, bufferSize),
		done: make(chan struct{}),
	}
	r.wg.Add(1)
	go r.run()
	return r, nil
}

// Record ставит сообщение в очередь записи. Никогда не блокируется: WS-цикл
// чтения раздаёт свечи блокирующе, и медленный диск не должен его останавливать.
func (r *Recorder) Record(recv time.Time, raw []byte) {
	select {
	case r.in <- message{recv: recv, raw: raw}:
	default:
		r.dropped.Add(1)
	}
}

// Dropped — сколько сообщений отброшено из-за переполнения буфера.
func (r *Recorder) Dropped() int64 { return r.dropped.Load() }

// Close дописывает очередь и закрывает файл.
func (r *Recorder) Close() {
	close(r.done)
	r.wg.Wait()
}

func (r *Recorder) run() {
	defer r.wg.Done()
	var f *dayFile
	defer func() {
		if f != nil {
			r.closeDay(f)
		}
	}()

	flush := time.NewTicker(flushEvery)
	defer flush.Stop()
	var lastDropped int64

	write := func(m message) {
		day := m.recv.In(r.loc).Format("2006-01-02")
		if f == nil || f.day != day {
			if f != nil {
				r.closeDay(f)
				f = nil
			}
			var err error
			if f, err = openDay(r.dir, day); err != nil {
				logx.Error("wsrecord: %v — сообщение потеряно", err)
				f = nil
				return
			}
		}
		if err := f.write(m); err != nil {
			logx.Error("wsrecord: запись %s: %v", f.path, err)
		}
	}

	for {
		select {
		case m := <-r.in:
			write(m)
		case <-flush.C:
			if f != nil {
				if err := f.flush(); err != nil {
					logx.Error("wsrecord: сброс %s: %v", f.path, err)
				}
			}
			if d := r.dropped.Load(); d != lastDropped {
				logx.Warn("wsrecord: отброшено сообщений за всё время: %d (буфер переполнен)", d)
				lastDropped = d
			}
		case <-r.done:
			for {
				select {
				case m := <-r.in:
					write(m)
				default:
					return
				}
			}
		}
	}
}

func (r *Recorder) closeDay(f *dayFile) {
	if err := f.close(); err != nil {
		logx.Error("wsrecord: закрытие %s: %v", f.path, err)
	}
	var size int64
	if st, err := os.Stat(f.path); err == nil {
		size = st.Size()
	}
	logx.Info("wsrecord: %s — %d сообщений за сессию записи, файл %.1f МБ, отброшено всего %d",
		filepath.Base(f.path), f.count, float64(size)/(1<<20), r.dropped.Load())
}

// dayFile — открытый файл одного дня: файл → gzip → bufio.
type dayFile struct {
	day   string
	path  string
	file  *os.File
	gz    *gzip.Writer
	buf   *bufio.Writer
	count int64
}

func openDay(dir, day string) (*dayFile, error) {
	path := filepath.Join(dir, day+".jsonl.gz")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("открытие %s: %w", path, err)
	}
	gz := gzip.NewWriter(file)
	return &dayFile{day: day, path: path, file: file, gz: gz, buf: bufio.NewWriterSize(gz, 64<<10)}, nil
}

type line struct {
	Recv string          `json:"recv"`
	Msg  json.RawMessage `json:"msg,omitempty"`
	Raw  string          `json:"raw,omitempty"`
}

func (f *dayFile) write(m message) error {
	l := line{Recv: m.recv.UTC().Format(time.RFC3339Nano)}
	if json.Valid(m.raw) {
		l.Msg = m.raw
	} else {
		l.Raw = string(m.raw)
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := f.buf.Write(b); err != nil {
		return err
	}
	f.count++
	return nil
}

func (f *dayFile) flush() error {
	if err := f.buf.Flush(); err != nil {
		return err
	}
	return f.gz.Flush()
}

func (f *dayFile) close() error {
	if err := f.buf.Flush(); err != nil {
		f.file.Close()
		return err
	}
	if err := f.gz.Close(); err != nil {
		f.file.Close()
		return err
	}
	return f.file.Close()
}
