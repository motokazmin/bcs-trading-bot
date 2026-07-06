package strategy

import "math"

// Params — набор числовых гиперпараметров (optimizer random search).
type Params map[string]float64

func (p Params) Int(key string) int {
	return int(math.Round(p[key]))
}

func (p Params) Float(key string) float64 {
	return p[key]
}

func (p Params) Bool(key string) bool {
	return p[key] >= 0.5
}

func (p Params) WithDefaults(defaults Params) Params {
	out := make(Params, len(defaults)+len(p))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range p {
		out[k] = v
	}
	return out
}
