package main

import "testing"

func TestBBoxFromSliceValid(t *testing.T) {
	got, err := bboxFromSlice([]float64{-123, 36, -121, 38})
	if err != nil {
		t.Fatalf("bboxFromSlice: %v", err)
	}
	if got.MinLon != -123 || got.MinLat != 36 || got.MaxLon != -121 || got.MaxLat != 38 {
		t.Errorf("got %+v, want {-123 36 -121 38}", got)
	}
}

func TestBBoxFromSliceRejectsWrongLength(t *testing.T) {
	if _, err := bboxFromSlice([]float64{-123, 36, -121}); err == nil {
		t.Fatal("want error for a 3-element bbox, got nil")
	}
}

func TestBBoxFromSliceRejectsInvalidOrdering(t *testing.T) {
	if _, err := bboxFromSlice([]float64{-121, 36, -123, 38}); err == nil {
		t.Fatal("want error for minLon >= maxLon, got nil")
	}
}
