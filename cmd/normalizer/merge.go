package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/aircrafttype"
	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

// freshnessTieWindow bounds what counts as a "near-tie" between reports'
// LastSeenUTC timestamps for the purposes of the multi-source merge
// precedence rule in docs/architecture/data-model.md. It matches the
// adapters' default poll interval (see e.g. cmd/adapter-airplaneslive),
// i.e. two reports from the same polling round should not have one
// spuriously "win" on freshness alone.
const freshnessTieWindow = 15 * time.Second

// Merge collapses every provider's report for one icao24 into a single
// canonical FlightState, per the multi-source merge precedence rule in
// docs/architecture/data-model.md:
//  1. The most recent report wins.
//  2. On a near-tie (within freshnessTieWindow), position quality
//     adsb > mlat > estimated breaks the tie.
//  3. Sources lists every provider that reported, regardless of which
//     report's fields won.
//
// A report whose payload fails to parse, or whose payload ICAO24 doesn't
// match icao24 (a mismatched/corrupt envelope), is dropped and does not
// block merging the rest; Merge only fails if none of raws parse.
func Merge(icao24 string, raws []sourceadapter.RawState) (flightmodel.FlightState, error) {
	if len(raws) == 0 {
		return flightmodel.FlightState{}, fmt.Errorf("normalizer: merge %s: no reports", icao24)
	}

	expectedICAO24 := strings.ToLower(icao24)
	reports := make([]providerReport, 0, len(raws))
	for _, raw := range raws {
		report, err := ParseRawState(raw)
		if err != nil {
			continue
		}
		if report.ICAO24 != expectedICAO24 {
			continue
		}
		reports = append(reports, report)
	}
	if len(reports) == 0 {
		return flightmodel.FlightState{}, fmt.Errorf("normalizer: merge %s: no reports parsed successfully", icao24)
	}

	winner := pickWinner(reports)
	typeSrc := pickTypeSource(reports, winner)
	iconClass := classifyIcon(typeSrc)

	return flightmodel.FlightState{
		ICAO24:          strings.ToLower(icao24),
		Callsign:        winner.Callsign,
		Registration:    winner.Registration,
		Lat:             winner.Lat,
		Lon:             winner.Lon,
		AltitudeBaroFt:  winner.AltitudeBaroFt,
		AltitudeGeoFt:   winner.AltitudeGeoFt,
		GroundSpeedKt:   winner.GroundSpeedKt,
		VerticalRateFpm: winner.VerticalRateFpm,
		HeadingDeg:      winner.HeadingDeg,
		OnGround:        winner.OnGround,
		Squawk:          winner.Squawk,
		Sources:         sourceList(reports),
		PositionQuality: winner.PositionQuality,
		LastSeenUTC:     winner.LastSeenUTC,
		AircraftType:    typeSrc.AircraftType,
		EmitterCategory: typeSrc.EmitterCategory,
		Military:        typeSrc.Military,
		IconClass:       iconClass,
	}, nil
}

// pickTypeSource selects which report supplies the aircraft type / category /
// military fields. A provider that supplies a type designator wins over one
// that does not (OpenSky never carries type), so type data survives even when
// the freshest positional report came from OpenSky. Among reports that carry a
// designator, the same precedence as pickWinner applies; if none do, the
// positional winner is used (it may still carry a military flag or category).
func pickTypeSource(reports []providerReport, winner providerReport) providerReport {
	typed := make([]providerReport, 0, len(reports))
	for _, r := range reports {
		if r.AircraftType != nil {
			typed = append(typed, r)
		}
	}
	if len(typed) == 0 {
		return winner
	}
	return pickWinner(typed)
}

// classifyIcon derives the icon bucket for a report's type fields, returning
// nil when nothing classifiable is available so the frontend draws a default.
func classifyIcon(r providerReport) *string {
	bucket := aircrafttype.Classify(r.AircraftType, r.EmitterCategory, r.Military)
	if bucket == "" {
		return nil
	}
	return &bucket
}

// MergeAll groups raws by ICAO24 and merges each group independently. A
// group that fails to merge (e.g. every report in it is malformed) is
// dropped rather than failing the whole batch, since one aircraft's bad
// data shouldn't block reporting on every other tracked aircraft.
func MergeAll(raws []sourceadapter.RawState) []flightmodel.FlightState {
	grouped := make(map[string][]sourceadapter.RawState)
	for _, raw := range raws {
		key := strings.ToLower(raw.ICAO24)
		grouped[key] = append(grouped[key], raw)
	}

	icao24s := make([]string, 0, len(grouped))
	for icao24 := range grouped {
		icao24s = append(icao24s, icao24)
	}
	sort.Strings(icao24s)

	states := make([]flightmodel.FlightState, 0, len(icao24s))
	for _, icao24 := range icao24s {
		state, err := Merge(icao24, grouped[icao24])
		if err != nil {
			continue
		}
		states = append(states, state)
	}
	return states
}

// pickWinner selects the report whose fields populate the merged
// FlightState, per the precedence rule documented on Merge. reports must
// be non-empty.
func pickWinner(reports []providerReport) providerReport {
	maxTime := reports[0].LastSeenUTC
	for _, r := range reports[1:] {
		if r.LastSeenUTC.After(maxTime) {
			maxTime = r.LastSeenUTC
		}
	}

	best := reports[0]
	bestIsCandidate := maxTime.Sub(best.LastSeenUTC) <= freshnessTieWindow
	for _, r := range reports[1:] {
		isCandidate := maxTime.Sub(r.LastSeenUTC) <= freshnessTieWindow
		switch {
		case !isCandidate:
			continue
		case !bestIsCandidate:
			best, bestIsCandidate = r, true
		case qualityRank(r.PositionQuality) < qualityRank(best.PositionQuality):
			best = r
		case qualityRank(r.PositionQuality) == qualityRank(best.PositionQuality):
			if r.LastSeenUTC.After(best.LastSeenUTC) ||
				(r.LastSeenUTC.Equal(best.LastSeenUTC) && r.Provider < best.Provider) {
				best = r
			}
		}
	}
	return best
}

// qualityRank orders PositionQuality from most to least trustworthy for
// tie-breaking, per docs/architecture/data-model.md.
func qualityRank(q flightmodel.PositionQuality) int {
	switch q {
	case flightmodel.PositionQualityADSB:
		return 0
	case flightmodel.PositionQualityMLAT:
		return 1
	default:
		return 2
	}
}

// sourceList returns the sorted, de-duplicated set of providers among
// reports, for FlightState.Sources.
func sourceList(reports []providerReport) []string {
	seen := make(map[string]bool, len(reports))
	sources := make([]string, 0, len(reports))
	for _, r := range reports {
		if !seen[r.Provider] {
			seen[r.Provider] = true
			sources = append(sources, r.Provider)
		}
	}
	sort.Strings(sources)
	return sources
}
