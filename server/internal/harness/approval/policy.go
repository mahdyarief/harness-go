package approval

import "hsg/internal/harness/services"

type Config struct{ Policy string }

func Decide(cfg Config, tool string) services.GuardDecision {
	if cfg.Policy == "ask" {
		return services.GuardDecision{Allowed: false, Reason: "alat " + tool + " memerlukan persetujuan"}
	}
	return services.GuardDecision{Allowed: true}
}
