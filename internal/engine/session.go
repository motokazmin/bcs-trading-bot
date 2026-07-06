package engine

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SessionClock определяет торговое окно и момент принудительного закрытия позиций.
type SessionClock struct {
	loc               *time.Location
	eodMinutes        int
	openMinutes       int
	entryDelayMinutes int
}

func NewSessionClock(timezone, eodClose, sessionOpen string, entryDelayMinutes int) (*SessionClock, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("часовой пояс %q: %w", timezone, err)
	}

	eodMinutes, err := parseHHMM(eodClose)
	if err != nil {
		return nil, fmt.Errorf("eod_close_time: %w", err)
	}
	openMinutes, err := parseHHMM(sessionOpen)
	if err != nil {
		return nil, fmt.Errorf("session_open_time: %w", err)
	}

	return &SessionClock{
		loc:               loc,
		eodMinutes:        eodMinutes,
		openMinutes:       openMinutes,
		entryDelayMinutes: entryDelayMinutes,
	}, nil
}

func parseHHMM(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("ожидается ЧЧ:ММ, получено %q", value)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("недопустимое время %q", value)
	}
	return h*60 + m, nil
}

func (s *SessionClock) nowMinutes(t time.Time) int {
	local := t.In(s.loc)
	return local.Hour()*60 + local.Minute()
}

func (s *SessionClock) today(t time.Time) string {
	return t.In(s.loc).Format("2006-01-02")
}

// Today возвращает торговую дату в формате YYYY-MM-DD.
func (s *SessionClock) Today(t time.Time) string {
	return s.today(t)
}

// EntriesAllowed возвращает false после eod_close_time, до session_open_time + entry_delay и до открытия сессии.
func (s *SessionClock) EntriesAllowed(now time.Time) bool {
	m := s.nowMinutes(now)
	if m >= s.eodMinutes {
		return false
	}
	if m < s.openMinutes+s.entryDelayMinutes {
		return false
	}
	return true
}

// ShouldForceClose возвращает true, если наступило время принудительного закрытия.
func (s *SessionClock) ShouldForceClose(now time.Time) bool {
	return s.nowMinutes(now) >= s.eodMinutes
}

// IsSessionOpen возвращает true, если наступило или прошло session_open_time.
func (s *SessionClock) IsSessionOpen(now time.Time) bool {
	return s.nowMinutes(now) >= s.openMinutes
}
