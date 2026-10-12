package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendКладётХостИТекст(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOK/sendMessage" || r.URL.Query().Get("chat_id") != "42" {
			t.Errorf("запрос: %s", r.URL)
		}
		got = r.URL.Query().Get("text")
	}))
	defer srv.Close()
	tg := &Telegram{token: "TOK", chat: "42", host: "vps", apiURL: srv.URL, httpClient: srv.Client()}
	if err := tg.send("токен истёк"); err != nil {
		t.Fatal(err)
	}
	if got != "[vps] бот: токен истёк" {
		t.Fatalf("text = %q", got)
	}
}

func TestSendНеСветитТокенВОшибке(t *testing.T) {
	tg := &Telegram{token: "SECRET", chat: "42", apiURL: "http://127.0.0.1:1", httpClient: http.DefaultClient}
	err := tg.send("x")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
}
