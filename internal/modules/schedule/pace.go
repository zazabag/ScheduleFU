package schedule

import "time"

// Pace — темп обхода: днём раз в Interval, ночью раз в NightInterval.
//
// Ночью расписание никто не открывает и деканат его не правит, а каждый
// проход — шесть сотен запросов к чужому серверу. Реже ночью — треть
// суточной нагрузки долой без потери для пользователя.
type Pace struct {
	Interval      time.Duration
	NightInterval time.Duration
	// NightFrom и NightTo — «ЧЧ:ММ» в поясе вуза; окно может переходить
	// через полночь. Пустые — ночного темпа нет.
	NightFrom, NightTo string
}

// Next — сколько ждать до следующего прохода, если предыдущий кончился в at.
func (p Pace) Next(at time.Time) time.Duration {
	if p.NightInterval > 0 && p.isNight(at.Format("15:04")) {
		return p.NightInterval
	}
	return p.Interval
}

// isNight сравнивает строки «ЧЧ:ММ»: при ведущих нулях лексический порядок
// совпадает со временным.
func (p Pace) isNight(hhmm string) bool {
	if p.NightFrom == "" || p.NightTo == "" || p.NightFrom == p.NightTo {
		return false
	}
	if p.NightFrom < p.NightTo {
		return hhmm >= p.NightFrom && hhmm < p.NightTo
	}
	return hhmm >= p.NightFrom || hhmm < p.NightTo
}
