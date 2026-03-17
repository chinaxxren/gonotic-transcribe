package service

import "github.com/chinaxxren/gonotic/internal/config"

// BillingParams contains runtime-configurable values that govern cycle durations
// and quota grants for the billing system.
type BillingParams struct {
	FreeCycleSeconds            int64
	FreeAllowanceSeconds        int
	PaygAllowanceSeconds        int
	SpecialOfferSeconds         int
	SpecialOfferAmountCents     int
	SpecialOfferValiditySeconds int64
	PaygValiditySeconds         int64
	PremiumCycleSeconds         int64
	PremiumCycleGrantSeconds    int
	ProCycleSeconds             int64
	ProTotalSeconds             int
	ProMiniCycleSeconds         int64
	ProMiniTotalSeconds         int

	SummaryFreeQuota         int
	SummaryPaygQuota         int
	SummarySpecialOfferQuota int
	SummaryPremiumQuota      int
	SummaryProQuota          int
	SummaryProMiniQuota      int
}

// DefaultBillingParams returns the legacy hard-coded configuration that existed
// before environment overrides were introduced.
func DefaultBillingParams() BillingParams {
	return BillingParams{
		FreeCycleSeconds:            30 * 24 * 3600,
		FreeAllowanceSeconds:        40 * 60,
		PaygAllowanceSeconds:        5 * 60 * 60,
		SpecialOfferSeconds:         10 * 60 * 60,
		SpecialOfferAmountCents:     990,
		SpecialOfferValiditySeconds: 30 * 24 * 3600,
		PaygValiditySeconds:         30 * 24 * 3600,
		PremiumCycleSeconds:         30 * 24 * 3600,
		PremiumCycleGrantSeconds:    1200 * 60,
		ProCycleSeconds:             360 * 24 * 3600,
		ProTotalSeconds:             800 * 60 * 60,
		ProMiniCycleSeconds:         360 * 24 * 3600,
		ProMiniTotalSeconds:         480 * 60 * 60,
		SummaryFreeQuota:            1,
		SummaryPaygQuota:            5,
		SummarySpecialOfferQuota:    10,
		SummaryPremiumQuota:         20,
		SummaryProQuota:             800,
		SummaryProMiniQuota:         480,
	}
}

// NewBillingParams constructs BillingParams from configuration, falling back to defaults
// when values are missing or invalid.
func NewBillingParams(cfg config.BillingConfig) BillingParams {
	params := DefaultBillingParams()

	if cfg.FreeCycleSeconds > 0 {
		params.FreeCycleSeconds = cfg.FreeCycleSeconds
	}
	if cfg.FreeAllowanceSeconds > 0 {
		params.FreeAllowanceSeconds = cfg.FreeAllowanceSeconds
	}
	if cfg.PaygAllowanceSeconds > 0 {
		params.PaygAllowanceSeconds = cfg.PaygAllowanceSeconds
	}
	if cfg.SpecialOfferSeconds > 0 {
		params.SpecialOfferSeconds = cfg.SpecialOfferSeconds
	}
	if cfg.SpecialOfferAmountCents > 0 {
		params.SpecialOfferAmountCents = cfg.SpecialOfferAmountCents
	}
	if cfg.SpecialOfferValiditySeconds > 0 {
		params.SpecialOfferValiditySeconds = cfg.SpecialOfferValiditySeconds
	}
	if cfg.PaygValiditySeconds > 0 {
		params.PaygValiditySeconds = cfg.PaygValiditySeconds
	}
	if cfg.PremiumCycleSeconds > 0 {
		params.PremiumCycleSeconds = cfg.PremiumCycleSeconds
	}
	if cfg.PremiumCycleGrantSeconds > 0 {
		params.PremiumCycleGrantSeconds = cfg.PremiumCycleGrantSeconds
	}
	if cfg.ProCycleSeconds > 0 {
		params.ProCycleSeconds = cfg.ProCycleSeconds
	}
	if cfg.ProTotalSeconds > 0 {
		params.ProTotalSeconds = cfg.ProTotalSeconds
	}
	if cfg.ProMiniCycleSeconds > 0 {
		params.ProMiniCycleSeconds = cfg.ProMiniCycleSeconds
	}
	if cfg.ProMiniTotalSeconds > 0 {
		params.ProMiniTotalSeconds = cfg.ProMiniTotalSeconds
	}

	return params
}

func NewBillingParamsWithSummary(cfg config.BillingConfig, summary config.SummaryConfig) BillingParams {
	params := NewBillingParams(cfg)
	if summary.FreeQuota > 0 {
		params.SummaryFreeQuota = summary.FreeQuota
	}
	if summary.PaygQuota > 0 {
		params.SummaryPaygQuota = summary.PaygQuota
	}
	if summary.SpecialOfferQuota > 0 {
		params.SummarySpecialOfferQuota = summary.SpecialOfferQuota
	}
	if summary.PremiumQuota > 0 {
		params.SummaryPremiumQuota = summary.PremiumQuota
	}
	if summary.ProQuota > 0 {
		params.SummaryProQuota = summary.ProQuota
	}
	if summary.ProMiniQuota > 0 {
		params.SummaryProMiniQuota = summary.ProMiniQuota
	}
	return params
}

// WithDefaults fills zero or negative values with legacy defaults.
func (p BillingParams) WithDefaults() BillingParams {
	defaults := DefaultBillingParams()

	if p.FreeCycleSeconds <= 0 {
		p.FreeCycleSeconds = defaults.FreeCycleSeconds
	}
	if p.FreeAllowanceSeconds <= 0 {
		p.FreeAllowanceSeconds = defaults.FreeAllowanceSeconds
	}
	if p.PaygAllowanceSeconds <= 0 {
		p.PaygAllowanceSeconds = defaults.PaygAllowanceSeconds
	}
	if p.SpecialOfferSeconds <= 0 {
		p.SpecialOfferSeconds = defaults.SpecialOfferSeconds
	}
	if p.SpecialOfferAmountCents <= 0 {
		p.SpecialOfferAmountCents = defaults.SpecialOfferAmountCents
	}
	if p.PaygValiditySeconds <= 0 {
		p.PaygValiditySeconds = defaults.PaygValiditySeconds
	}
	if p.PremiumCycleSeconds <= 0 {
		p.PremiumCycleSeconds = defaults.PremiumCycleSeconds
	}
	if p.PremiumCycleGrantSeconds <= 0 {
		p.PremiumCycleGrantSeconds = defaults.PremiumCycleGrantSeconds
	}
	if p.ProCycleSeconds <= 0 {
		p.ProCycleSeconds = defaults.ProCycleSeconds
	}
	if p.ProTotalSeconds <= 0 {
		p.ProTotalSeconds = defaults.ProTotalSeconds
	}
	if p.ProMiniCycleSeconds <= 0 {
		p.ProMiniCycleSeconds = defaults.ProMiniCycleSeconds
	}
	if p.ProMiniTotalSeconds <= 0 {
		p.ProMiniTotalSeconds = defaults.ProMiniTotalSeconds
	}
	if p.SummaryFreeQuota <= 0 {
		p.SummaryFreeQuota = defaults.SummaryFreeQuota
	}
	if p.SummaryPaygQuota <= 0 {
		p.SummaryPaygQuota = defaults.SummaryPaygQuota
	}
	if p.SummarySpecialOfferQuota <= 0 {
		p.SummarySpecialOfferQuota = defaults.SummarySpecialOfferQuota
	}
	if p.SummaryPremiumQuota <= 0 {
		p.SummaryPremiumQuota = defaults.SummaryPremiumQuota
	}
	if p.SummaryProQuota <= 0 {
		p.SummaryProQuota = defaults.SummaryProQuota
	}
	if p.SummaryProMiniQuota <= 0 {
		p.SummaryProMiniQuota = defaults.SummaryProMiniQuota
	}

	return p
}

// PremiumAnnualSeconds returns the effective annual validity window for premium
// subscriptions, assuming 12 cycles per year.
func (p BillingParams) PremiumAnnualSeconds() int64 {
	return p.PremiumCycleSeconds * 12
}
