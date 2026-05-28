// Package fitconv converts FIT files recorded with Onelap (which uses
// the Chinese GCJ-02 coordinate system) into the international WGS-84
// coordinate system that Strava expects.
package fitconv

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/tormoder/fit"
)

const (
	gcjA  = 6378245.0
	gcjEE = 0.00669342162296594323
)

// outOfChina returns true when the WGS-84 (lat, lon) lies outside mainland
// China's bounding box. GCJ-02 is only applied within China, so coordinates
// outside this box should be left unchanged.
func outOfChina(lat, lon float64) bool {
	if lon < 72.004 || lon > 137.8347 {
		return true
	}
	if lat < 0.8293 || lat > 55.8271 {
		return true
	}
	return false
}

func transformLat(x, y float64) float64 {
	ret := -100.0 + 2.0*x + 3.0*y + 0.2*y*y + 0.1*x*y + 0.2*math.Sqrt(math.Abs(x))
	ret += (20.0*math.Sin(6.0*x*math.Pi) + 20.0*math.Sin(2.0*x*math.Pi)) * 2.0 / 3.0
	ret += (20.0*math.Sin(y*math.Pi) + 40.0*math.Sin(y/3.0*math.Pi)) * 2.0 / 3.0
	ret += (160.0*math.Sin(y/12.0*math.Pi) + 320*math.Sin(y*math.Pi/30.0)) * 2.0 / 3.0
	return ret
}

func transformLon(x, y float64) float64 {
	ret := 300.0 + x + 2.0*y + 0.1*x*x + 0.1*x*y + 0.1*math.Sqrt(math.Abs(x))
	ret += (20.0*math.Sin(6.0*x*math.Pi) + 20.0*math.Sin(2.0*x*math.Pi)) * 2.0 / 3.0
	ret += (20.0*math.Sin(x*math.Pi) + 40.0*math.Sin(x/3.0*math.Pi)) * 2.0 / 3.0
	ret += (150.0*math.Sin(x/12.0*math.Pi) + 300.0*math.Sin(x/30.0*math.Pi)) * 2.0 / 3.0
	return ret
}

// gcjDelta returns the (dLat, dLon) offset that, when added to a WGS-84
// coordinate, produces the corresponding GCJ-02 coordinate.
func gcjDelta(lat, lon float64) (float64, float64) {
	dLat := transformLat(lon-105.0, lat-35.0)
	dLon := transformLon(lon-105.0, lat-35.0)
	radLat := lat / 180.0 * math.Pi
	magic := math.Sin(radLat)
	magic = 1 - gcjEE*magic*magic
	sqrtMagic := math.Sqrt(magic)
	dLat = (dLat * 180.0) / ((gcjA * (1 - gcjEE)) / (magic * sqrtMagic) * math.Pi)
	dLon = (dLon * 180.0) / (gcjA / sqrtMagic * math.Cos(radLat) * math.Pi)
	return dLat, dLon
}

// gcj02ToWGS84 converts a single GCJ-02 coordinate to WGS-84 by iteratively
// inverting the forward transform. A handful of iterations converges to
// well below 1e-7 degrees (~1 cm).
func gcj02ToWGS84(gcjLat, gcjLon float64) (float64, float64) {
	if outOfChina(gcjLat, gcjLon) {
		return gcjLat, gcjLon
	}
	wgsLat, wgsLon := gcjLat, gcjLon
	for i := 0; i < 5; i++ {
		dLat, dLon := gcjDelta(wgsLat, wgsLon)
		wgsLat = gcjLat - dLat
		wgsLon = gcjLon - dLon
	}
	return wgsLat, wgsLon
}

// ConvertFile reads the FIT file at path, converts every GPS coordinate
// from GCJ-02 to WGS-84, and writes the result back to the same path.
func ConvertFile(path string) error {
	in, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open fit file: %w", err)
	}
	file, err := fit.Decode(in)
	in.Close()
	if err != nil {
		return fmt.Errorf("decode fit file: %w", err)
	}

	if file.Type() != fit.FileTypeActivity {
		// Only activity files contain GPS records we care about.
		return nil
	}

	activity, err := file.Activity()
	if err != nil {
		return fmt.Errorf("extract activity: %w", err)
	}

	convertLatLon := func(lat fit.Latitude, lon fit.Longitude) (fit.Latitude, fit.Longitude) {
		if lat.Invalid() || lon.Invalid() {
			return lat, lon
		}
		newLat, newLon := gcj02ToWGS84(lat.Degrees(), lon.Degrees())
		return fit.NewLatitudeDegrees(newLat), fit.NewLongitudeDegrees(newLon)
	}

	for _, r := range activity.Records {
		r.PositionLat, r.PositionLong = convertLatLon(r.PositionLat, r.PositionLong)
	}
	for _, l := range activity.Laps {
		l.StartPositionLat, l.StartPositionLong = convertLatLon(l.StartPositionLat, l.StartPositionLong)
		l.EndPositionLat, l.EndPositionLong = convertLatLon(l.EndPositionLat, l.EndPositionLong)
	}
	for _, s := range activity.Sessions {
		s.StartPositionLat, s.StartPositionLong = convertLatLon(s.StartPositionLat, s.StartPositionLong)
		s.NecLat, s.NecLong = convertLatLon(s.NecLat, s.NecLong)
		s.SwcLat, s.SwcLong = convertLatLon(s.SwcLat, s.SwcLong)
	}

	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("open fit file for write: %w", err)
	}
	defer out.Close()
	if err := fit.Encode(out, file, binary.LittleEndian); err != nil {
		return fmt.Errorf("encode fit file: %w", err)
	}
	return nil
}
