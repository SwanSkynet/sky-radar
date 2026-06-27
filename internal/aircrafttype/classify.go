// Package aircrafttype maps a captured aircraft type designator, ADS-B
// emitter category, and military flag onto one of the frontend's icon
// buckets. The buckets are the SVG file names in web/src/assets.
//
// This is a deliberately small seed classifier (see Feature 2 in
// docs/features/batch-1-coverage-detail-icons-cadence.md). A comprehensive
// ICAO type-designator reference DB and military hex-range tables are a
// later batch; until then, designators not in the seed table fall back to
// the default icon (the empty bucket), and military aircraft without a more
// specific match route to a generic military icon.
package aircrafttype

import "strings"

// Icon buckets. Each constant equals the corresponding SVG file name (minus
// extension) in web/src/assets.
const (
	BucketCommercialJet     = "commercial_jet"
	BucketWidebody          = "widebody"
	BucketBusinessJet       = "business_jet"
	BucketCargo             = "cargo"
	BucketTurboprop         = "turboprop"
	BucketUtilityHelicopter = "utility_helicopter"
	BucketAttackHelicopter  = "attack_helicopter"
	BucketMilitaryTransport = "military_transport"
	BucketMilitaryDrone     = "military_drone"
	BucketTanker            = "tanker"
	BucketAWACS             = "awacs"
	BucketReconnaissance    = "reconnaissance"
	BucketMaritimePatrol    = "maritime_patrol"
	BucketStealth           = "stealth"
)

// genericMilitary is the bucket used for a military-flagged aircraft that
// doesn't match a more specific military type. military_transport is the
// most neutral fixed-wing military icon in the asset set.
const genericMilitary = BucketMilitaryTransport

// Classify returns the icon bucket for an aircraft, or "" when nothing
// classifiable is available (the caller / frontend then draws a default).
//
// Priority, per the feature spec:
//  1. military flag → specific military bucket by designator, else a
//     generic military icon (helicopters split attack vs utility).
//  2. emitter category → rotorcraft (utility helicopter) / heavy (widebody).
//  3. civil designator → civil bucket via the seed designator table.
//  4. fallback → "" (unknown).
func Classify(typeDesignator, emitterCategory *string, military bool) string {
	t := normalizeDesignator(typeDesignator)
	cat := normalizeCategory(emitterCategory)

	if military {
		if b := militaryBucket(t, cat); b != "" {
			return b
		}
		return genericMilitary
	}

	if b := categoryBucket(cat); b != "" {
		return b
	}

	return civilBucket(t)
}

// militaryBucket maps a military aircraft's designator (and category, for
// rotorcraft) onto a specific military bucket, or "" to fall back to a
// generic military icon.
func militaryBucket(t, cat string) string {
	switch {
	case t == "":
		// No designator: only rotorcraft can still be distinguished.
		if cat == "A7" {
			return BucketUtilityHelicopter
		}
		return ""
	// AWACS / airborne early warning.
	case hasPrefix(t, "E3"), t == "E767", hasPrefix(t, "E2"):
		return BucketAWACS
	// Aerial refueling tankers.
	case hasPrefix(t, "KC"):
		return BucketTanker
	// Unmanned.
	case t == "RQ4", hasPrefix(t, "MQ"):
		return BucketMilitaryDrone
	// Reconnaissance (check RC before generic C-transports below).
	case t == "U2", hasPrefix(t, "RC"):
		return BucketReconnaissance
	// Maritime patrol.
	case t == "P8", hasPrefix(t, "P3"), t == "P1":
		return BucketMaritimePatrol
	// Low-observable / stealth.
	case t == "F22", t == "F35", t == "B2":
		return BucketStealth
	// Transports.
	case t == "C17", hasPrefix(t, "C13"), t == "A400", t == "C5", t == "C5M":
		return BucketMilitaryTransport
	// Rotorcraft: attack vs utility by designator, else category.
	case hasPrefix(t, "AH"):
		return BucketAttackHelicopter
	case hasPrefix(t, "UH"), hasPrefix(t, "CH"), hasPrefix(t, "HH"), hasPrefix(t, "MH"):
		return BucketUtilityHelicopter
	case cat == "A7":
		return BucketUtilityHelicopter
	default:
		return ""
	}
}

// categoryBucket maps an ADS-B emitter category onto a bucket for civil
// aircraft, or "" when the category is not decisive.
func categoryBucket(cat string) string {
	switch cat {
	case "A7": // Rotorcraft.
		return BucketUtilityHelicopter
	case "A5": // Heavy (> 300,000 lb).
		return BucketWidebody
	default:
		return ""
	}
}

// civilBucket maps a civil ICAO type designator onto a bucket via the seed
// table, or "" when the designator is not yet seeded.
func civilBucket(t string) string {
	if t == "" {
		return ""
	}
	switch {
	// Freighters (explicit -F / known cargo variants) before the passenger
	// families they share a prefix with.
	case hasSuffix(t, "F") && (hasPrefix(t, "B7") || hasPrefix(t, "A3") || hasPrefix(t, "MD")):
		return BucketCargo
	case t == "B77L", t == "B748", t == "B74F", t == "B77F":
		return BucketCargo
	// Widebodies. A310 is matched here explicitly so it is not swept up by
	// the A31x narrowbody (A318/A319) prefixes below.
	case t == "A310",
		hasPrefix(t, "A30"), hasPrefix(t, "A33"), hasPrefix(t, "A34"), hasPrefix(t, "A35"),
		hasPrefix(t, "A38"),
		hasPrefix(t, "B74"), hasPrefix(t, "B76"), hasPrefix(t, "B77"), hasPrefix(t, "B78"),
		t == "MD11", t == "IL96":
		return BucketWidebody
	// Narrowbody commercial jets.
	case hasPrefix(t, "A318"), hasPrefix(t, "A319"), hasPrefix(t, "A32"),
		hasPrefix(t, "A19"), hasPrefix(t, "A20"), hasPrefix(t, "A21"), hasPrefix(t, "A22"),
		hasPrefix(t, "BCS"),
		hasPrefix(t, "B71"), hasPrefix(t, "B72"), hasPrefix(t, "B73"), hasPrefix(t, "B75"),
		hasPrefix(t, "E17"), hasPrefix(t, "E19"), hasPrefix(t, "E29"),
		hasPrefix(t, "CRJ"), hasPrefix(t, "MD8"), hasPrefix(t, "MD9"):
		return BucketCommercialJet
	// Business jets.
	case hasPrefix(t, "GLF"), hasPrefix(t, "LJ"), hasPrefix(t, "C25"), hasPrefix(t, "C5"),
		hasPrefix(t, "C68"), hasPrefix(t, "C70"), hasPrefix(t, "CL3"), hasPrefix(t, "CL6"),
		hasPrefix(t, "F2TH"), hasPrefix(t, "FA"), hasPrefix(t, "E55"), t == "GALX",
		t == "H25B", t == "BE40", t == "PRM1":
		return BucketBusinessJet
	// Turboprops.
	case hasPrefix(t, "DH8"), hasPrefix(t, "AT4"), hasPrefix(t, "AT5"), hasPrefix(t, "AT7"),
		hasPrefix(t, "B19"), hasPrefix(t, "SF3"), hasPrefix(t, "C208"), hasPrefix(t, "PC12"),
		hasPrefix(t, "E110"), hasPrefix(t, "E120"), hasPrefix(t, "DHC"):
		return BucketTurboprop
	default:
		return ""
	}
}

func normalizeDesignator(s *string) string {
	if s == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(*s))
}

func normalizeCategory(s *string) string {
	if s == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(*s))
}

func hasPrefix(s, prefix string) bool { return strings.HasPrefix(s, prefix) }
func hasSuffix(s, suffix string) bool { return strings.HasSuffix(s, suffix) }
