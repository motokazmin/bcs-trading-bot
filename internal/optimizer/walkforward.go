package optimizer

import "time"

// Window — одно walk-forward окно.
type Window struct {
	TrainStart, TrainEnd time.Time
	TestStart, TestEnd   time.Time
}

// GenerateWindows разбивает период на скользящие train/test окна.
func GenerateWindows(fullStart, fullEnd time.Time, trainMonths, testMonths, stepMonths int) []Window {
	if trainMonths <= 0 || testMonths <= 0 || stepMonths <= 0 {
		return nil
	}
	if !fullStart.Before(fullEnd) {
		return nil
	}

	var windows []Window
	cursor := fullStart

	for {
		trainEnd := addMonths(cursor, trainMonths)
		testStart := trainEnd
		testEnd := addMonths(testStart, testMonths)

		if testEnd.After(fullEnd) {
			break
		}

		windows = append(windows, Window{
			TrainStart: cursor,
			TrainEnd:   trainEnd,
			TestStart:  testStart,
			TestEnd:    testEnd,
		})

		cursor = addMonths(cursor, stepMonths)
		if !cursor.Before(fullEnd) {
			break
		}
		// следующее окно должно иметь полный train-период
		nextTrainEnd := addMonths(cursor, trainMonths)
		if nextTrainEnd.After(fullEnd) {
			break
		}
	}

	return windows
}

func addMonths(t time.Time, months int) time.Time {
	return t.AddDate(0, months, 0)
}
