package aircrafttype

import "testing"

func strptr(s string) *string { return &s }

func TestClassify(t *testing.T) {
	tests := []struct {
		name      string
		designent *string
		category  *string
		military  bool
		want      string
	}{
		// Military — specific buckets.
		{"awacs E3", strptr("E3TF"), nil, true, BucketAWACS},
		{"awacs E2 hawkeye", strptr("E2"), nil, true, BucketAWACS},
		{"tanker KC135", strptr("KC135"), nil, true, BucketTanker},
		{"drone RQ4", strptr("RQ4"), nil, true, BucketMilitaryDrone},
		{"drone MQ9", strptr("MQ9"), nil, true, BucketMilitaryDrone},
		{"recon U2", strptr("U2"), nil, true, BucketReconnaissance},
		{"recon RC135", strptr("RC135"), nil, true, BucketReconnaissance},
		{"maritime P8", strptr("P8"), nil, true, BucketMaritimePatrol},
		{"stealth F35", strptr("F35"), nil, true, BucketStealth},
		{"stealth B2", strptr("B2"), nil, true, BucketStealth},
		{"transport C17", strptr("C17"), nil, true, BucketMilitaryTransport},
		{"transport C130", strptr("C130"), nil, true, BucketMilitaryTransport},
		{"attack helo AH64", strptr("AH64"), nil, true, BucketAttackHelicopter},
		{"utility helo UH60", strptr("UH60"), nil, true, BucketUtilityHelicopter},
		{"military rotor by category", nil, strptr("A7"), true, BucketUtilityHelicopter},
		{"generic military fallback", strptr("SU27"), nil, true, genericMilitary},
		{"generic military no designator", nil, nil, true, genericMilitary},

		// Civil — category-driven.
		{"civil rotorcraft category", strptr("EC35"), strptr("A7"), false, BucketUtilityHelicopter},
		{"heavy category widebody", nil, strptr("A5"), false, BucketWidebody},

		// Civil — designator table.
		{"narrowbody A320", strptr("A320"), nil, false, BucketCommercialJet},
		{"narrowbody B738", strptr("B738"), nil, false, BucketCommercialJet},
		{"embraer ejet E190", strptr("E190"), nil, false, BucketCommercialJet},
		{"widebody A359", strptr("A359"), nil, false, BucketWidebody},
		{"widebody B789", strptr("B789"), nil, false, BucketWidebody},
		{"widebody B763", strptr("B763"), nil, false, BucketWidebody},
		{"business jet GLF6", strptr("GLF6"), nil, false, BucketBusinessJet},
		{"business jet C560", strptr("C560"), nil, false, BucketBusinessJet},
		{"cargo B77F", strptr("B77F"), nil, false, BucketCargo},
		{"cargo B748 freighter variant", strptr("B748"), nil, false, BucketCargo},
		{"turboprop DH8D", strptr("DH8D"), nil, false, BucketTurboprop},
		{"turboprop AT76", strptr("AT76"), nil, false, BucketTurboprop},

		// Unknown / fallback.
		{"unknown designator", strptr("ZZZZ"), nil, false, ""},
		{"all nil", nil, nil, false, ""},
		{"case-insensitive lowercase", strptr("a320"), nil, false, BucketCommercialJet},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.designent, tc.category, tc.military)
			if got != tc.want {
				t.Errorf("Classify(%v, %v, %v) = %q, want %q", deref(tc.designent), deref(tc.category), tc.military, got, tc.want)
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
