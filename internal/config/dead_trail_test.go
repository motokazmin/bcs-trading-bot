package config_test

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// Трейлинг мёртв, если активация выше тейка: позиция закроется по тейку раньше,
// чем трейл включится, и trail_stage_max / trail_breakeven_r становятся декорацией.
//
// Это не теория. У orc-complement переподбор сдвинул активацию 1.515 → 0.364
// (с 92% пути к тейку на 30%) и дал PF портфеля 2.18 → 2.80. В слотах ниже рычаг
// не просто хуже настроен — он выключен. См. docs/analysis/0003-*.
//
// Тест характеризующий: фиксирует ИЗВЕСТНЫЕ мёртвые слоты и падает, если появится
// новый или если список разойдётся с конфигом. Чинится переподбором выхода, а не
// правкой числа наугад — произвольная активация меняет поведение невалидированно.
// В paper-m15 известных нет: активация подбиралась долей тейка (of: rewardRatio, 0014–0015).
// У выведенного M5-стенда было три (configs/legacy/runs/portfolio-paper-m5.yaml).
func TestМёртвыйТрейлингТолькоВИзвестныхСлотах(t *testing.T) {
	known := map[string]string{}

	raw, err := os.ReadFile("../../configs/runs/paper-m15.yaml")
	if err != nil {
		t.Fatalf("чтение конфига: %v", err)
	}
	var cfg struct {
		Experiments []struct {
			ID       string `yaml:"id"`
			Strategy struct {
				RewardRatio       float64 `yaml:"reward_ratio"`
				TrailActivationR  float64 `yaml:"trail_activation_r"`
				TakeProfitEnabled *bool   `yaml:"take_profit_enabled"`
			} `yaml:"strategy"`
		} `yaml:"experiments"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("разбор конфига: %v", err)
	}

	seen := map[string]bool{}
	for _, e := range cfg.Experiments {
		st := e.Strategy
		if st.TakeProfitEnabled != nil && !*st.TakeProfitEnabled {
			continue // тейка нет — активации не с чем конкурировать
		}
		if st.RewardRatio <= 0 || st.TrailActivationR <= 0 {
			continue
		}
		if st.TrailActivationR < st.RewardRatio {
			continue // трейл достижим
		}
		seen[e.ID] = true
		if _, ok := known[e.ID]; !ok {
			t.Errorf("%s: НОВЫЙ мёртвый трейлинг — активация %.4f >= тейк %.4f. "+
				"trail_stage_max и trail_breakeven_r в этом слоте ни на что не влияют",
				e.ID, st.TrailActivationR, st.RewardRatio)
		}
	}
	for id, why := range known {
		if !seen[id] {
			t.Errorf("%s больше не мёртв (%s) — убрать из списка известных, "+
				"иначе он перестанет ловить регрессию", id, why)
		}
	}
}
