package app

import (
	"testing"
	"time"
)

func TestRefreshTokenExpiryAlertЗа14Дней(t *testing.T) {
	exp := time.Date(2027, 1, 10, 14, 59, 0, 0, time.UTC)
	if _, due := refreshTokenExpiryAlert(exp, exp.Add(-15*24*time.Hour)); due {
		t.Fatal("тревога за 15 дней")
	}
	if _, due := refreshTokenExpiryAlert(exp, exp.Add(-13*24*time.Hour)); !due {
		t.Fatal("нет тревоги за 13 дней")
	}
	if _, due := refreshTokenExpiryAlert(exp, exp.Add(time.Hour)); !due {
		t.Fatal("нет тревоги об истёкшем токене")
	}
}
