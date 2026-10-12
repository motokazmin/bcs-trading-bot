package broker

import (
	"errors"
	"testing"
	"time"
)

func TestStreamAlarmОтказАвторизацииСразуИРазВЧас(t *testing.T) {
	var a streamAlarm
	t0 := moscowTime(10, 12)
	err := errors.New("Token is not active")
	if _, ok := a.down(t0, err, true); !ok {
		t.Fatal("отказ авторизации без тревоги")
	}
	if _, ok := a.down(t0.Add(30*time.Minute), err, true); ok {
		t.Fatal("тревога каждую минуту — спам")
	}
	if _, ok := a.down(t0.Add(61*time.Minute), err, true); !ok {
		t.Fatal("нет напоминания через час")
	}
	if msg, ok := a.up(t0.Add(2 * time.Hour)); !ok || msg == "" {
		t.Fatal("нет отбоя после тревоги")
	}
}

func TestStreamAlarmКороткийОбрывМолчит(t *testing.T) {
	var a streamAlarm
	t0 := moscowTime(12, 0)
	err := errors.New("close 1006")
	if _, ok := a.down(t0, err, false); ok {
		t.Fatal("тревога на первом же обрыве")
	}
	if _, ok := a.up(t0.Add(5 * time.Second)); ok {
		t.Fatal("отбой без тревоги")
	}
	if _, ok := a.down(t0.Add(time.Hour), err, false); ok {
		t.Fatal("простой считается от прошлого обрыва, а не от текущего")
	}
	if _, ok := a.down(t0.Add(time.Hour+11*time.Minute), err, false); !ok {
		t.Fatal("поток лежит 11 минут днём — тревоги нет")
	}
}

func TestStreamAlarmНочьюСетевойОбрывМолчит(t *testing.T) {
	var a streamAlarm
	t0 := moscowTime(1, 0)
	err := errors.New("close 1006")
	a.down(t0, err, false)
	if _, ok := a.down(t0.Add(time.Hour), err, false); ok {
		t.Fatal("ночью без торгов тревога на обрыв")
	}
}
