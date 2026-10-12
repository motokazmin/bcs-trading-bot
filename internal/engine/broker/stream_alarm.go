package broker

import (
	"fmt"
	"time"
)

const (
	// streamDownAlertAfter — сколько поток может лежать по сетевой причине до
	// тревоги: обрывы с переподключением за секунды — норма, их не шлём.
	streamDownAlertAfter = 10 * time.Minute
	// streamAlertRepeat — напоминание, пока поток так и не поднялся.
	streamAlertRepeat = time.Hour
)

// streamAlarm решает, когда звать человека, пока WS лежит. Отказ
// переавторизации — сразу: сам он не пройдёт, нужен новый refresh token
// (2026-10-11 бот так простоял ~32 ч без данных, в Telegram ничего не пришло).
// Прочие сбои — если поток лежит дольше streamDownAlertAfter вне ночного окна.
type streamAlarm struct {
	downSince time.Time
	lastAlert time.Time
}

// down — очередная неудачная попытка. Возвращает текст тревоги, если пора слать.
func (a *streamAlarm) down(now time.Time, err error, authFailed bool) (string, bool) {
	if a.downSince.IsZero() {
		a.downSince = now
	}
	if !a.lastAlert.IsZero() && now.Sub(a.lastAlert) < streamAlertRepeat {
		return "", false
	}
	since := now.Sub(a.downSince).Round(time.Minute)
	var msg string
	switch {
	case authFailed:
		msg = fmt.Sprintf("БКС не принимает BCS_REFRESH_TOKEN, рыночных данных нет %s. "+
			"Выпустить новый токен, прописать в /etc/trading-bot/env, systemctl restart trading-bot. Ошибка: %v", since, err)
	case since >= streamDownAlertAfter && !isWSQuietPeriod(now):
		msg = fmt.Sprintf("WS рыночных данных лежит %s, переподключение не проходит: %v", since, err)
	default:
		return "", false
	}
	a.lastAlert = now
	return msg, true
}

// up — сессия поднялась. Тревога была — сообщаем, что отбой.
func (a *streamAlarm) up(now time.Time) (string, bool) {
	alerted := !a.lastAlert.IsZero()
	since := now.Sub(a.downSince).Round(time.Minute)
	*a = streamAlarm{}
	if !alerted {
		return "", false
	}
	return fmt.Sprintf("WS рыночных данных восстановлен, простой %s", since), true
}
