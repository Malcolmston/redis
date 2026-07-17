package redis

import (
	"math"
	"sort"
)

// Geospatial encoding constants. A geo key is an ordinary sorted set whose
// score is a 52-bit geohash produced by interleaving a 26-bit-quantized
// longitude and latitude. The ranges and Earth radius match Redis exactly so
// encoded scores, distances, and hashes are interchangeable with it.
const (
	geoLonMin      = -180.0
	geoLonMax      = 180.0
	geoLatMin      = -85.05112878
	geoLatMax      = 85.05112878
	geoStep        = 26
	geoEarthRadius = 6372797.560856
)

// geoAlphabet is the base32 alphabet Redis uses for GEOHASH replies.
const geoAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz"

// GeoPoint is a longitude/latitude coordinate in degrees.
type GeoPoint struct {
	// Longitude is the east-west coordinate in degrees, range -180..180.
	Longitude float64
	// Latitude is the north-south coordinate in degrees, range
	// -85.05112878..85.05112878.
	Latitude float64
}

// GeoMember pairs a coordinate with a member name for insertion via GeoAdd.
type GeoMember struct {
	// Longitude is the east-west coordinate in degrees.
	Longitude float64
	// Latitude is the north-south coordinate in degrees.
	Latitude float64
	// Member is the sorted-set member name to associate with the coordinate.
	Member string
}

// GeoUnit is a distance unit expressed as its length in meters. Multiply a
// value in the unit by the GeoUnit to obtain meters, or divide meters by it to
// convert back.
type GeoUnit float64

const (
	// GeoMeters is the meter unit.
	GeoMeters GeoUnit = 1
	// GeoKilometers is the kilometer unit (1000 meters).
	GeoKilometers GeoUnit = 1000
	// GeoMiles is the statute-mile unit (1609.34 meters).
	GeoMiles GeoUnit = 1609.34
	// GeoFeet is the foot unit (0.3048 meters).
	GeoFeet GeoUnit = 0.3048
)

// GeoSearchResult is one match returned by GeoSearch or GeoRadius.
type GeoSearchResult struct {
	// Member is the matched sorted-set member.
	Member string
	// Dist is the distance from the search center in the requested unit.
	Dist float64
	// Point is the member's decoded coordinate.
	Point GeoPoint
}

// GeoAdd adds each member at its coordinate to the geo set at key, encoding the
// coordinate as a 52-bit geohash score and delegating to ZAdd. It returns the
// number of members newly added; coordinate updates to existing members are
// not counted, matching GEOADD.
func (s *Store) GeoAdd(key string, members ...GeoMember) (int, error) {
	zms := make([]ZMember, len(members))
	for i, m := range members {
		zms[i] = ZMember{Member: m.Member, Score: geoEncodeScore(m.Longitude, m.Latitude)}
	}
	return s.ZAdd(key, zms...)
}

// GeoPos returns the decoded coordinate of each requested member. The returned
// slice has one entry per member in order; the entry is nil when the key or the
// member is absent.
func (s *Store) GeoPos(key string, members ...string) ([]*GeoPoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil {
		return nil, err
	}
	out := make([]*GeoPoint, len(members))
	for i, m := range members {
		if it == nil {
			continue
		}
		sc, ok := it.zset.score(m)
		if !ok {
			continue
		}
		lon, lat := geoDecodeScore(uint64(sc))
		out[i] = &GeoPoint{Longitude: lon, Latitude: lat}
	}
	return out, nil
}

// GeoDist returns the geodesic distance between members m1 and m2 in the given
// unit, computed with the haversine formula. The boolean is false (and the
// distance zero) when the key or either member is absent.
func (s *Store) GeoDist(key, m1, m2 string, unit GeoUnit) (float64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, false, err
	}
	s1, ok1 := it.zset.score(m1)
	s2, ok2 := it.zset.score(m2)
	if !ok1 || !ok2 {
		return 0, false, nil
	}
	lon1, lat1 := geoDecodeScore(uint64(s1))
	lon2, lat2 := geoDecodeScore(uint64(s2))
	meters := geoHaversine(lon1, lat1, lon2, lat2)
	return meters / float64(unit), true, nil
}

// GeoHash returns the 11-character base32 geohash of each requested member,
// using the Redis-compatible alphabet and the standard -90..90 latitude range.
// The returned slice has one entry per member; the entry is the empty string
// when the key or member is absent.
func (s *Store) GeoHash(key string, members ...string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(members))
	for i, m := range members {
		if it == nil {
			continue
		}
		sc, ok := it.zset.score(m)
		if !ok {
			continue
		}
		lon, lat := geoDecodeScore(uint64(sc))
		out[i] = geoHashString(lon, lat)
	}
	return out, nil
}

// GeoSearch returns members within radius of center, implementing GEOSEARCH
// BYRADIUS FROMLONLAT. It scans the sorted set in score order, decodes each
// member, and keeps those whose haversine distance from center is within
// radius (both interpreted in unit). A count of zero or less is unlimited;
// otherwise at most count results are returned. When sortAsc is true results
// are ordered by ascending distance (ties broken by member) before count is
// applied.
func (s *Store) GeoSearch(key string, center GeoPoint, radius float64, unit GeoUnit, count int, sortAsc bool) ([]GeoSearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []GeoSearchResult{}, err
	}
	radiusMeters := radius * float64(unit)
	out := make([]GeoSearchResult, 0)
	for _, m := range it.zset.sl.toSlice() {
		lon, lat := geoDecodeScore(uint64(m.Score))
		meters := geoHaversine(center.Longitude, center.Latitude, lon, lat)
		if meters > radiusMeters {
			continue
		}
		out = append(out, GeoSearchResult{
			Member: m.Member,
			Dist:   meters / float64(unit),
			Point:  GeoPoint{Longitude: lon, Latitude: lat},
		})
	}
	if sortAsc {
		sort.Slice(out, func(i, j int) bool {
			if out[i].Dist != out[j].Dist {
				return out[i].Dist < out[j].Dist
			}
			return out[i].Member < out[j].Member
		})
	}
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	return out, nil
}

// GeoRadius is the legacy GEORADIUS spelling; it wraps GeoSearch with the
// center given as explicit longitude and latitude.
func (s *Store) GeoRadius(key string, lon, lat, radius float64, unit GeoUnit, count int, sortAsc bool) ([]GeoSearchResult, error) {
	return s.GeoSearch(key, GeoPoint{Longitude: lon, Latitude: lat}, radius, unit, count, sortAsc)
}

// geoEncodeScore quantizes lon/lat to 26 bits each and interleaves them into a
// 52-bit geohash returned as a float64 score (exactly representable).
func geoEncodeScore(lon, lat float64) float64 {
	scale := float64(uint64(1) << geoStep)
	latOffset := (lat - geoLatMin) / (geoLatMax - geoLatMin)
	lonOffset := (lon - geoLonMin) / (geoLonMax - geoLonMin)
	ilat := uint32(latOffset * scale)
	ilon := uint32(lonOffset * scale)
	return float64(geoInterleave(ilat, ilon))
}

// geoDecodeScore reverses geoEncodeScore, returning the center coordinate of
// the geohash cell identified by the 52-bit score.
func geoDecodeScore(bits uint64) (lon, lat float64) {
	scale := float64(uint64(1) << geoStep)
	ilat, ilon := geoDeinterleave(bits)
	latLo := geoLatMin + (float64(ilat)/scale)*(geoLatMax-geoLatMin)
	latHi := geoLatMin + (float64(ilat+1)/scale)*(geoLatMax-geoLatMin)
	lonLo := geoLonMin + (float64(ilon)/scale)*(geoLonMax-geoLonMin)
	lonHi := geoLonMin + (float64(ilon+1)/scale)*(geoLonMax-geoLonMin)
	return (lonLo + lonHi) / 2, (latLo + latHi) / 2
}

// geoInterleave spreads the latitude bits into the even positions and the
// longitude bits into the odd positions of a 52-bit value.
func geoInterleave(xlat, ylon uint32) uint64 {
	masks := [...]uint64{
		0x5555555555555555,
		0x3333333333333333,
		0x0F0F0F0F0F0F0F0F,
		0x00FF00FF00FF00FF,
		0x0000FFFF0000FFFF,
	}
	shifts := [...]uint{1, 2, 4, 8, 16}
	x := uint64(xlat)
	y := uint64(ylon)
	x = (x | (x << shifts[4])) & masks[4]
	x = (x | (x << shifts[3])) & masks[3]
	x = (x | (x << shifts[2])) & masks[2]
	x = (x | (x << shifts[1])) & masks[1]
	x = (x | (x << shifts[0])) & masks[0]
	y = (y | (y << shifts[4])) & masks[4]
	y = (y | (y << shifts[3])) & masks[3]
	y = (y | (y << shifts[2])) & masks[2]
	y = (y | (y << shifts[1])) & masks[1]
	y = (y | (y << shifts[0])) & masks[0]
	return x | (y << 1)
}

// geoDeinterleave is the inverse of geoInterleave, recovering the quantized
// latitude (even bits) and longitude (odd bits).
func geoDeinterleave(bits uint64) (xlat, ylon uint32) {
	masks := [...]uint64{
		0x5555555555555555,
		0x3333333333333333,
		0x0F0F0F0F0F0F0F0F,
		0x00FF00FF00FF00FF,
		0x0000FFFF0000FFFF,
		0x00000000FFFFFFFF,
	}
	shifts := [...]uint{0, 1, 2, 4, 8, 16}
	x := bits
	y := bits >> 1
	x = (x | (x >> shifts[0])) & masks[0]
	x = (x | (x >> shifts[1])) & masks[1]
	x = (x | (x >> shifts[2])) & masks[2]
	x = (x | (x >> shifts[3])) & masks[3]
	x = (x | (x >> shifts[4])) & masks[4]
	x = (x | (x >> shifts[5])) & masks[5]
	y = (y | (y >> shifts[0])) & masks[0]
	y = (y | (y >> shifts[1])) & masks[1]
	y = (y | (y >> shifts[2])) & masks[2]
	y = (y | (y >> shifts[3])) & masks[3]
	y = (y | (y >> shifts[4])) & masks[4]
	y = (y | (y >> shifts[5])) & masks[5]
	return uint32(x), uint32(y)
}

// geoHashString re-encodes a coordinate with the standard geohash latitude
// range (-90..90) and formats the 52-bit result as 11 base32 characters,
// matching the Redis GEOHASH reply.
func geoHashString(lon, lat float64) string {
	scale := float64(uint64(1) << geoStep)
	latOffset := (lat - (-90.0)) / (90.0 - (-90.0))
	lonOffset := (lon - geoLonMin) / (geoLonMax - geoLonMin)
	ilat := uint32(latOffset * scale)
	ilon := uint32(lonOffset * scale)
	bits := geoInterleave(ilat, ilon)
	buf := make([]byte, 11)
	for i := 0; i < 11; i++ {
		var idx uint64
		if i == 10 {
			// Only 52 bits are available; the final character is padded.
			idx = 0
		} else {
			idx = (bits >> uint(52-(i+1)*5)) & 0x1f
		}
		buf[i] = geoAlphabet[idx]
	}
	return string(buf)
}

// geoHaversine returns the great-circle distance in meters between two
// coordinates using the haversine formula and the Redis Earth radius.
func geoHaversine(lon1, lat1, lon2, lat2 float64) float64 {
	lat1r := geoDeg2Rad(lat1)
	lat2r := geoDeg2Rad(lat2)
	u := math.Sin((lat2r - lat1r) / 2)
	v := math.Sin((geoDeg2Rad(lon2) - geoDeg2Rad(lon1)) / 2)
	a := u*u + math.Cos(lat1r)*math.Cos(lat2r)*v*v
	return 2.0 * geoEarthRadius * math.Asin(math.Sqrt(a))
}

// geoDeg2Rad converts degrees to radians.
func geoDeg2Rad(deg float64) float64 { return deg * math.Pi / 180.0 }
