package wsrecord

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var msk = time.FixedZone("MSK", 3*3600)

type readLine struct {
	Recv string          `json:"recv"`
	Msg  json.RawMessage `json:"msg"`
	Raw  string          `json:"raw"`
}

func readDay(t *testing.T, path string) []readLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f) // многочленный gzip читается подряд по умолчанию
	if err != nil {
		t.Fatal(err)
	}
	var out []readLine
	sc := bufio.NewScanner(gz)
	for sc.Scan() {
		var l readLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("битая строка %q: %v", sc.Text(), err)
		}
		out = append(out, l)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestЗаписьСохраняетСообщениеКакЕстьИВремяПолучения(t *testing.T) {
	dir := t.TempDir()
	r, err := New(dir, msk)
	if err != nil {
		t.Fatal(err)
	}
	recv := time.Date(2026, 9, 23, 7, 30, 1, 123456789, time.UTC)
	raw := []byte(`{"responseType":"CandleStick","ticker":"CHMF","close":627.6}`)
	r.Record(recv, raw)
	r.Record(recv, []byte("не json"))
	r.Close()

	lines := readDay(t, filepath.Join(dir, "2026-09-23.jsonl.gz"))
	if len(lines) != 2 {
		t.Fatalf("строк %d, want 2", len(lines))
	}
	if string(lines[0].Msg) != string(raw) {
		t.Fatalf("сообщение изменено при записи: %s", lines[0].Msg)
	}
	if lines[0].Recv != "2026-09-23T07:30:01.123456789Z" {
		t.Fatalf("время получения потеряло точность: %s", lines[0].Recv)
	}
	if lines[1].Raw != "не json" {
		t.Fatalf("не-JSON сообщение потеряно: %+v", lines[1])
	}
}

// День файла — по МСК: 23:30 UTC 22-го — это уже 23-е, торговая сессия не режется.
func TestДеньФайлаПоМосковскомуВремени(t *testing.T) {
	dir := t.TempDir()
	r, err := New(dir, msk)
	if err != nil {
		t.Fatal(err)
	}
	r.Record(time.Date(2026, 9, 22, 20, 59, 0, 0, time.UTC), []byte(`{"a":1}`)) // 23:59 МСК 22-го
	r.Record(time.Date(2026, 9, 22, 21, 1, 0, 0, time.UTC), []byte(`{"a":2}`))  // 00:01 МСК 23-го
	r.Close()

	if n := len(readDay(t, filepath.Join(dir, "2026-09-22.jsonl.gz"))); n != 1 {
		t.Fatalf("22-е: %d строк, want 1", n)
	}
	if n := len(readDay(t, filepath.Join(dir, "2026-09-23.jsonl.gz"))); n != 1 {
		t.Fatalf("23-е: %d строк, want 1", n)
	}
}

// Рестарт бота в тот же день не должен затирать уже записанное.
func TestРестартДописываетФайлДня(t *testing.T) {
	dir := t.TempDir()
	recv := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		r, err := New(dir, msk)
		if err != nil {
			t.Fatal(err)
		}
		r.Record(recv, []byte(`{"run":1}`))
		r.Close()
	}
	if n := len(readDay(t, filepath.Join(dir, "2026-09-23.jsonl.gz"))); n != 2 {
		t.Fatalf("после рестарта строк %d, want 2 — первый прогон затёрт", n)
	}
}

// Record не блокируется, даже если писатель не успевает: торговля важнее записи.
func TestRecordНеБлокируетсяПриПереполнении(t *testing.T) {
	r := &Recorder{in: make(chan message, 1), done: make(chan struct{})} // писатель не запущен
	finished := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			r.Record(time.Now(), []byte(`{}`))
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Record заблокировал вызывающего при полном буфере")
	}
	if r.Dropped() != 9 {
		t.Fatalf("dropped=%d, want 9", r.Dropped())
	}
}
