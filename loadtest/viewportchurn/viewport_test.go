package main

import "testing"

func TestNewSimulatedViewportDeterministic(t *testing.T) {
	a := newSimulatedViewport(7)
	b := newSimulatedViewport(7)
	if a.centerLon != b.centerLon || a.centerLat != b.centerLat || a.halfSpanDeg != b.halfSpanDeg {
		t.Fatalf("same-seed viewports differ: %+v vs %+v", a, b)
	}
}

func TestNewSimulatedViewportStartsAtAHotspot(t *testing.T) {
	v := newSimulatedViewport(1)
	for _, h := range hotspots {
		if v.centerLon == h.centerLon && v.centerLat == h.lat {
			return
		}
	}
	t.Fatalf("viewport center (%v, %v) does not match any hotspot", v.centerLon, v.centerLat)
}

func TestChurnProducesValidBBox(t *testing.T) {
	v := newSimulatedViewport(3)
	for i := 0; i < 200; i++ {
		bbox := v.churn()
		if err := bbox.Validate(); err != nil {
			t.Fatalf("churn step %d produced invalid bbox %+v: %v", i, bbox, err)
		}
	}
}

func TestChurnStaysWithinConfiguredSpan(t *testing.T) {
	v := newSimulatedViewport(9)
	for i := 0; i < 200; i++ {
		v.churn()
		if v.halfSpanDeg < minHalfSpanDeg || v.halfSpanDeg > maxHalfSpanDeg {
			t.Fatalf("halfSpanDeg = %v out of [%v, %v] after step %d", v.halfSpanDeg, minHalfSpanDeg, maxHalfSpanDeg, i)
		}
		if v.centerLat < -85 || v.centerLat > 85 {
			t.Fatalf("centerLat = %v out of range after step %d", v.centerLat, i)
		}
	}
}

func TestWrapLon(t *testing.T) {
	cases := map[float64]float64{
		0:    0,
		190:  -170,
		-190: 170,
		180:  180,
		-180: -180,
	}
	for in, want := range cases {
		if got := wrapLon(in); got != want {
			t.Errorf("wrapLon(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestClamp(t *testing.T) {
	if got := clamp(5, 0, 10); got != 5 {
		t.Errorf("clamp(5,0,10) = %v, want 5", got)
	}
	if got := clamp(-5, 0, 10); got != 0 {
		t.Errorf("clamp(-5,0,10) = %v, want 0", got)
	}
	if got := clamp(15, 0, 10); got != 10 {
		t.Errorf("clamp(15,0,10) = %v, want 10", got)
	}
}
