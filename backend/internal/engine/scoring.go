package engine

import (
	"fmt"
	"math"
	"strconv"

	"draftside/internal/domain"
)

const materialScoringEdge = 0.02

// scoringModel deliberately stays small: Sleeper's player search rank is a
// market-order prior, not a statistical projection. These bounded position
// factors only break close cross-position ties relative to a common half-PPR,
// four-point passing-touchdown baseline.
type scoringModel struct {
	receptions       float64
	hasReceptions    bool
	passingTouchdown float64
	hasPassingTD     bool
	tightEndPremium  float64
	hasTEPremium     bool
}

func newScoringModel(settings map[string]float64) scoringModel {
	model := scoringModel{}
	if value, ok := finiteSetting(settings, "rec"); ok {
		model.receptions, model.hasReceptions = value, true
	}
	if value, ok := finiteSetting(settings, "pass_td"); ok {
		model.passingTouchdown, model.hasPassingTD = value, true
	}
	if value, ok := finiteSetting(settings, "bonus_rec_te"); ok {
		model.tightEndPremium, model.hasTEPremium = value, true
	}
	return model
}

func finiteSetting(settings map[string]float64, key string) (float64, bool) {
	value, ok := settings[key]
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func (model scoringModel) factor(position domain.Position) float64 {
	factor := 1.0
	if model.hasReceptions {
		delta := clamp(model.receptions-0.5, -0.5, 1.0)
		switch position {
		case domain.RB:
			factor += delta * 0.05
		case domain.WR:
			factor += delta * 0.08
		case domain.TE:
			factor += delta * 0.07
		}
	}
	if model.hasPassingTD && position == domain.QB {
		factor += clamp(model.passingTouchdown-4, -2, 3) * 0.025
	}
	if model.hasTEPremium && position == domain.TE {
		factor += clamp(model.tightEndPremium, -1, 2) * 0.06
	}
	return clamp(factor, 0.88, 1.12)
}

func (model scoringModel) reasonForChoice(selected domain.Position, alternatives []domain.Position) (Reason, bool) {
	selectedFactor := model.factor(selected)
	materialEdge := false
	for _, alternative := range alternatives {
		if selectedFactor-model.factor(alternative) >= materialScoringEdge {
			materialEdge = true
			break
		}
	}
	if !materialEdge {
		return Reason{}, false
	}

	if model.hasTEPremium && selected == domain.TE && model.tightEndPremium > 0 {
		return Reason{
			Label:  "League scoring fit",
			Detail: fmt.Sprintf("The %s-point tight-end reception premium adds a modest position-level tiebreaker; player-specific receiving projections are not yet modeled.", formatPoints(model.tightEndPremium)),
		}, true
	}
	if model.hasReceptions && model.receptions > 0.5 && (selected == domain.RB || selected == domain.WR || selected == domain.TE) {
		return Reason{
			Label:  "League scoring fit",
			Detail: fmt.Sprintf("%s scoring gives %s a modest position-level tiebreaker in close comparisons; player-specific reception projections are not yet modeled.", receptionLabel(model.receptions), selected),
		}, true
	}
	if model.hasPassingTD && model.passingTouchdown > 4 && selected == domain.QB {
		return Reason{
			Label:  "League scoring fit",
			Detail: fmt.Sprintf("%s-point passing touchdowns give QB a modest position-level tiebreaker in close comparisons; player-specific passing projections are not yet modeled.", formatPoints(model.passingTouchdown)),
		}, true
	}
	if model.hasReceptions && model.receptions < 0.5 && selected == domain.QB {
		return Reason{
			Label:  "League scoring fit",
			Detail: fmt.Sprintf("%s scoring modestly reduces the position-level reception premium in close comparisons; player-specific reception projections are not yet modeled.", receptionLabel(model.receptions)),
		}, true
	}
	if model.hasPassingTD && model.passingTouchdown < 4 && selected != domain.QB {
		return Reason{
			Label:  "League scoring fit",
			Detail: fmt.Sprintf("%s-point passing touchdowns modestly reduce QB's position-level weight in close comparisons; player-specific passing projections are not yet modeled.", formatPoints(model.passingTouchdown)),
		}, true
	}
	return Reason{}, false
}

func receptionLabel(points float64) string {
	switch {
	case math.Abs(points-1) < 0.001:
		return "Full-PPR"
	case math.Abs(points-0.5) < 0.001:
		return "Half-PPR"
	case math.Abs(points) < 0.001:
		return "Non-PPR"
	default:
		return formatPoints(points) + " PPR"
	}
}

func formatPoints(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}
