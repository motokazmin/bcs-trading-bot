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
	weekdaysOnly      bool // только пн–пт
	weekendOnly       bool // только сб–вс (ДСВД)
}

func NewSessionClock(timezone, eodClose, sessionOpen string, entryDelayMinutes int) (*SessionClock, error) {
	return NewSessionClockExt(timezone, eodClose, sessionOpen, entryDelayMinutes, false, false)
}

// NewSessionClockExt — SessionClock с фильтром дней недели.
func NewSessionClockExt(timezone, eodClose, sessionOpen string, entryDelayMinutes int, weekdaysOnly, weekendOnly bool) (*SessionClock, error) {
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
	if weekdaysOnly && weekendOnly {
		return nil, fmt.Errorf("session: weekdays_only и weekend_only взаимоисключающи")
	}

	return &SessionClock{
		loc:               loc,
		eodMinutes:        eodMinutes,
		openMinutes:       openMinutes,
		entryDelayMinutes: entryDelayMinutes,
		weekdaysOnly:      weekdaysOnly,
		weekendOnly:       weekendOnly,
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

func (s *SessionClock) dayAllowed(now time.Time) bool {
	wd := now.In(s.loc).Weekday()
	isWeekend := wd == time.Saturday || wd == time.Sunday
	if s.weekendOnly && !isWeekend {
		return false
	}
	if s.weekdaysOnly && isWeekend {
		return false
	}
	return true
}

// EntriesAllowed возвращает false после eod_close_time, до session_open_time + entry_delay и до открытия сессии.
func (s *SessionClock) EntriesAllowed(now time.Time) bool {
	if !s.dayAllowed(now) {
		return false
	}
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
	if !s.dayAllowed(now) {
		return false
	}
	return s.nowMinutes(now) >= s.eodMinutes
}

// IsSessionOpen возвращает true, если наступило или прошло session_open_time.
func (s *SessionClock) IsSessionOpen(now time.Time) bool {
	if !s.dayAllowed(now) {
		return false
	}
	return s.nowMinutes(now) >= s.openMinutes
}
