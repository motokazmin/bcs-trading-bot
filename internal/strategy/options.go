package strategy

const (
	StopModeRange = "range"
	StopModeATR   = "atr"

	defaultATRPeriod     = 14
	defaultATRMultiplier = 2.0
	defaultRangeCapPct   = 0.5
	defaultVolumeMinRatio = 1.5
)

// Options задаёт параметры стратегии MomentumBreakout.
type Options struct {
	Lookback           int
	StopMode           string
	ATRPeriod          int
	ATRMultiplier      float64
	RewardRatio        float64
	RangeUseCap        bool
	VolumeFilter       bool
	VolumeMinRatio     float64
	BreakoutThreshold  float64 // доля над/под уровнем (0 = чистый пробой)
}

func (o Options) normalized() Options {
	out := o
	if out.Lookback < 2 {
		out.Lookback = defaultLookback
	}
	if out.StopMode == "" {
		out.StopMode = StopModeRange
	}
	if out.ATRPeriod < 2 {
		out.ATRPeriod = defaultATRPeriod
	}
	if out.ATRMultiplier <= 0 {
		out.ATRMultiplier = defaultATRMultiplier
	}
	if out.RewardRatio <= 0 {
		out.RewardRatio = defaultRiskRewardRatio
	}
	if out.VolumeMinRatio <= 0 {
		out.VolumeMinRatio = defaultVolumeMinRatio
	}
	return out
}
