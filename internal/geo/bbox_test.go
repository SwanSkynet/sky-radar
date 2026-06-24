package geo

import "testing"

func TestParseBBoxValid(t *testing.T) {
	got, err := ParseBBox("-122.5,37.5,-122.0,38.0")
	if err != nil {
		t.Fatalf("ParseBBox: %v", err)
	}
	want := BBox{MinLon: -122.5, MinLat: 37.5, MaxLon: -122.0, MaxLat: 38.0}
	if got != want {
		t.Errorf("ParseBBox = %+v, want %+v", got, want)
	}
}

func TestParseBBoxRejectsWrongFieldCount(t *testing.T) {
	if _, err := ParseBBox("1,2,3"); err == nil {
		t.Fatal("ParseBBox(3 fields): want error, got nil")
	}
}

func TestParseBBoxRejectsNonNumeric(t *testing.T) {
	if _, err := ParseBBox("a,2,3,4"); err == nil {
		t.Fatal("ParseBBox(non-numeric): want error, got nil")
	}
}

func TestParseBBoxRejectsOutOfRangeCoordinates(t *testing.T) {
	cases := []string{
		"-200,0,1,1",
		"0,0,200,1",
		"0,-100,1,1",
		"0,0,1,100",
	}
	for _, s := range cases {
		if _, err := ParseBBox(s); err == nil {
			t.Errorf("ParseBBox(%q): want error, got nil", s)
		}
	}
}

func TestParseBBoxRejectsInvertedBounds(t *testing.T) {
	if _, err := ParseBBox("1,0,0,1"); err == nil {
		t.Fatal("ParseBBox(minLon > maxLon): want error, got nil")
	}
	if _, err := ParseBBox("0,1,1,0"); err == nil {
		t.Fatal("ParseBBox(minLat > maxLat): want error, got nil")
	}
}

func TestBBoxContains(t *testing.T) {
	b := BBox{MinLon: -10, MinLat: -10, MaxLon: 10, MaxLat: 10}

	if !b.Contains(0, 0) {
		t.Error("Contains(0,0): want true")
	}
	if !b.Contains(10, 10) {
		t.Error("Contains(10,10): want true (inclusive edge)")
	}
	if b.Contains(20, 0) {
		t.Error("Contains(20,0): want false")
	}
	if b.Contains(0, 20) {
		t.Error("Contains(0,20): want false")
	}
}

func TestBBoxCenter(t *testing.T) {
	b := BBox{MinLon: -10, MinLat: -10, MaxLon: 10, MaxLat: 30}
	lon, lat := b.Center()
	if lon != 0 || lat != 10 {
		t.Errorf("Center() = (%v, %v), want (0, 10)", lon, lat)
	}
}

func TestBBoxRadiusKMCoversCorners(t *testing.T) {
	b := BBox{MinLon: -1, MinLat: -1, MaxLon: 1, MaxLat: 1}
	r := b.RadiusKM()
	if r <= 0 {
		t.Fatalf("RadiusKM() = %v, want > 0", r)
	}
	// Sanity bound: a ~2x2 degree box should have a corner-radius on the
	// order of ~150km, far less than, say, half the earth's circumference.
	if r > 1000 {
		t.Errorf("RadiusKM() = %v, want a small radius for a 2x2 degree box", r)
	}
}
