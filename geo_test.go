package redis

import (
	"math"
	"testing"
)

// sicilyStore returns a store seeded with the two cities from the Redis GEO
// documentation, which have well-known reference distances and geohashes.
func sicilyStore(t *testing.T) *Store {
	t.Helper()
	s := New()
	n, err := s.GeoAdd("Sicily",
		GeoMember{Longitude: 13.361389, Latitude: 38.115556, Member: "Palermo"},
		GeoMember{Longitude: 15.087269, Latitude: 37.502669, Member: "Catania"},
	)
	if err != nil {
		t.Fatalf("GeoAdd: %v", err)
	}
	if n != 2 {
		t.Fatalf("GeoAdd added = %d, want 2", n)
	}
	return s
}

func TestGeoAddCountsNewOnly(t *testing.T) {
	s := sicilyStore(t)
	// Re-adding Palermo with a new position is an update, not an add.
	n, err := s.GeoAdd("Sicily",
		GeoMember{Longitude: 13.4, Latitude: 38.2, Member: "Palermo"},
		GeoMember{Longitude: 12.5, Latitude: 41.9, Member: "Rome"},
	)
	if err != nil {
		t.Fatalf("GeoAdd: %v", err)
	}
	if n != 1 {
		t.Errorf("GeoAdd added = %d, want 1", n)
	}
}

func TestGeoPos(t *testing.T) {
	s := sicilyStore(t)
	pos, err := s.GeoPos("Sicily", "Palermo", "Catania", "NonExisting")
	if err != nil {
		t.Fatalf("GeoPos: %v", err)
	}
	if len(pos) != 3 {
		t.Fatalf("GeoPos len = %d, want 3", len(pos))
	}
	if pos[2] != nil {
		t.Errorf("GeoPos[NonExisting] = %+v, want nil", pos[2])
	}
	want := []GeoPoint{
		{Longitude: 13.361389, Latitude: 38.115556},
		{Longitude: 15.087269, Latitude: 37.502669},
	}
	for i, w := range want {
		if pos[i] == nil {
			t.Fatalf("GeoPos[%d] = nil, want %+v", i, w)
		}
		if math.Abs(pos[i].Longitude-w.Longitude) > 1e-4 {
			t.Errorf("GeoPos[%d].Longitude = %v, want ~%v", i, pos[i].Longitude, w.Longitude)
		}
		if math.Abs(pos[i].Latitude-w.Latitude) > 1e-4 {
			t.Errorf("GeoPos[%d].Latitude = %v, want ~%v", i, pos[i].Latitude, w.Latitude)
		}
	}
}

func TestGeoPosMissingKey(t *testing.T) {
	s := New()
	pos, err := s.GeoPos("nope", "a", "b")
	if err != nil {
		t.Fatalf("GeoPos: %v", err)
	}
	for i, p := range pos {
		if p != nil {
			t.Errorf("GeoPos[%d] = %+v, want nil", i, p)
		}
	}
}

func TestGeoDist(t *testing.T) {
	s := sicilyStore(t)
	tests := []struct {
		name string
		unit GeoUnit
		want float64
		tol  float64
	}{
		{"meters", GeoMeters, 166274.1516, 1.0},
		{"kilometers", GeoKilometers, 166.2742, 0.01},
		{"miles", GeoMiles, 103.3182, 0.01},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := s.GeoDist("Sicily", "Palermo", "Catania", tt.unit)
			if err != nil {
				t.Fatalf("GeoDist: %v", err)
			}
			if !ok {
				t.Fatal("GeoDist ok = false, want true")
			}
			if math.Abs(got-tt.want) > tt.tol {
				t.Errorf("GeoDist = %v, want ~%v", got, tt.want)
			}
		})
	}
}

func TestGeoDistMissing(t *testing.T) {
	s := sicilyStore(t)
	if _, ok, err := s.GeoDist("Sicily", "Palermo", "Nope", GeoMeters); err != nil || ok {
		t.Errorf("GeoDist missing member: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
	if _, ok, err := s.GeoDist("Absent", "Palermo", "Catania", GeoMeters); err != nil || ok {
		t.Errorf("GeoDist missing key: ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

func TestGeoHash(t *testing.T) {
	s := sicilyStore(t)
	got, err := s.GeoHash("Sicily", "Palermo", "Catania", "NonExisting")
	if err != nil {
		t.Fatalf("GeoHash: %v", err)
	}
	want := []string{"sqc8b49rny0", "sqdtr74hyu0", ""}
	if len(got) != len(want) {
		t.Fatalf("GeoHash len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GeoHash[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGeoSearch(t *testing.T) {
	s := sicilyStore(t)
	center := GeoPoint{Longitude: 15, Latitude: 37}

	// A wide radius sorted by distance returns Catania (near center) first.
	res, err := s.GeoSearch("Sicily", center, 200, GeoKilometers, 0, true)
	if err != nil {
		t.Fatalf("GeoSearch: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("GeoSearch returned %d, want 2", len(res))
	}
	if res[0].Member != "Catania" || res[1].Member != "Palermo" {
		t.Errorf("GeoSearch order = [%q %q], want [Catania Palermo]", res[0].Member, res[1].Member)
	}
	if !(res[0].Dist < res[1].Dist) {
		t.Errorf("GeoSearch distances not ascending: %v then %v", res[0].Dist, res[1].Dist)
	}
	// The Dist field is expressed in the requested unit (kilometers here).
	if res[0].Dist > 200 || res[0].Dist <= 0 {
		t.Errorf("GeoSearch[0].Dist = %v km, out of expected range", res[0].Dist)
	}

	// count limits the result set after sorting.
	limited, err := s.GeoSearch("Sicily", center, 200, GeoKilometers, 1, true)
	if err != nil {
		t.Fatalf("GeoSearch count: %v", err)
	}
	if len(limited) != 1 || limited[0].Member != "Catania" {
		t.Errorf("GeoSearch count=1 = %+v, want [Catania]", limited)
	}

	// A tight radius excludes the far city.
	near, err := s.GeoSearch("Sicily", center, 100, GeoKilometers, 0, true)
	if err != nil {
		t.Fatalf("GeoSearch near: %v", err)
	}
	if len(near) != 1 || near[0].Member != "Catania" {
		t.Errorf("GeoSearch 100km = %+v, want [Catania]", near)
	}
}

func TestGeoRadiusMatchesGeoSearch(t *testing.T) {
	s := sicilyStore(t)
	center := GeoPoint{Longitude: 15, Latitude: 37}
	a, err := s.GeoRadius("Sicily", 15, 37, 200, GeoKilometers, 0, true)
	if err != nil {
		t.Fatalf("GeoRadius: %v", err)
	}
	b, err := s.GeoSearch("Sicily", center, 200, GeoKilometers, 0, true)
	if err != nil {
		t.Fatalf("GeoSearch: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("GeoRadius len %d != GeoSearch len %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Member != b[i].Member {
			t.Errorf("mismatch at %d: %q vs %q", i, a[i].Member, b[i].Member)
		}
	}
}

func TestGeoEncodeDecodeRoundTrip(t *testing.T) {
	points := []GeoPoint{
		{Longitude: 13.361389, Latitude: 38.115556},
		{Longitude: 0, Latitude: 0},
		{Longitude: -122.4194, Latitude: 37.7749},
		{Longitude: 139.6917, Latitude: 35.6895},
	}
	for _, p := range points {
		bits := uint64(geoEncodeScore(p.Longitude, p.Latitude))
		lon, lat := geoDecodeScore(bits)
		if math.Abs(lon-p.Longitude) > 1e-4 {
			t.Errorf("lon round trip = %v, want ~%v", lon, p.Longitude)
		}
		if math.Abs(lat-p.Latitude) > 1e-4 {
			t.Errorf("lat round trip = %v, want ~%v", lat, p.Latitude)
		}
	}
}
