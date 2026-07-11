package core

import "time"

// Window — одно walk-forward окно оценки.
type Window struct {
	Start, End time.Time
}

// GenerateWindows разбивает период на скользящие окна оценки.
func GenerateWindows(fullStart, fullEnd time.Time, windowMonths, stepMonths int) []Window {
	if windowMonths <= 0 || stepMonths <= 0 {
		return nil
	}
	if !fullStart.Before(fullEnd) {
		return nil
	}

	var windows []Window
	cursor := fullStart

	for {
		end := addMonths(cursor, windowMonths)
		if end.After(fullEnd) {
			break
		}

		windows = append(windows, Window{
			Start: cursor,
			End:   end,
		})

		cursor = addMonths(cursor, stepMonths)
		if !cursor.Before(fullEnd) {
			break
		}
		nextEnd := addMonths(cursor, windowMonths)
		if nextEnd.After(fullEnd) {
			break
		}
	}

	return windows
}

func addMonths(t time.Time, months int) time.Time {
	return t.AddDate(0, months, 0)
}
