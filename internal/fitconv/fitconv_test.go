package fitconv

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/tormoder/fit"
	"github.com/tormoder/fit/dyncrc16"
)

func TestConvertFilePatchesCoordinatesAndPreservesDeveloperData(t *testing.T) {
	wgsLat, wgsLon := 40.05293794953961, 116.29024463881879
	gcjLat, gcjLon := wgs84ToGCJ02ForTest(wgsLat, wgsLon)
	in := buildMinimalActivityFITForTest(t, gcjLat, gcjLon)

	path := filepath.Join(t.TempDir(), "activity.fit")
	if err := os.WriteFile(path, in, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := ConvertFile(path); err != nil {
		t.Fatalf("ConvertFile() error = %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read converted: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("converted FIT length = %d, want original length %d", len(out), len(in))
	}
	if !bytes.Contains(out, []byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatal("developer data payload was not preserved")
	}
	if dyncrc16.Checksum(out) != 0 {
		t.Fatal("converted FIT has invalid CRC")
	}

	decoded, err := fit.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("converted FIT does not decode: %v", err)
	}
	activity, err := decoded.Activity()
	if err != nil {
		t.Fatalf("converted FIT is not an activity: %v", err)
	}
	if len(activity.Records) != 1 {
		t.Fatalf("converted records = %d, want 1", len(activity.Records))
	}
	assertNearMeters(t, "record position", wgsLat, wgsLon, activity.Records[0].PositionLat.Degrees(), activity.Records[0].PositionLong.Degrees(), 2)
	if len(activity.Sessions) != 1 {
		t.Fatalf("converted sessions = %d, want 1", len(activity.Sessions))
	}
	assertNearMeters(t, "session end", wgsLat, wgsLon, activity.Sessions[0].EndPositionLat.Degrees(), activity.Sessions[0].EndPositionLong.Degrees(), 2)
}

func buildMinimalActivityFITForTest(t *testing.T, gcjLat, gcjLon float64) []byte {
	t.Helper()

	latSemi := fit.NewLatitudeDegrees(gcjLat).Semicircles()
	lonSemi := fit.NewLongitudeDegrees(gcjLon).Semicircles()

	var data bytes.Buffer

	// file_id definition + data: file type activity.
	data.Write([]byte{
		0x40,       // definition, local message 0
		0x00,       // reserved
		0x00,       // little endian
		0x00, 0x00, // global message 0 (file_id)
		0x01,             // one field
		0x00, 0x01, 0x00, // field 0 type, size 1, enum
		0x00, // data, local message 0
		0x04, // file type activity
	})

	// record definition with one developer data field. The converter should
	// patch the normal coordinate fields without dropping or rewriting this.
	data.Write([]byte{
		0x61,       // definition with developer fields, local message 1
		0x00,       // reserved
		0x00,       // little endian
		0x14, 0x00, // global message 20 (record)
		0x02,             // two normal fields
		0x00, 0x04, 0x85, // position_lat sint32
		0x01, 0x04, 0x85, // position_long sint32
		0x01,             // one developer field
		0x00, 0x04, 0x00, // developer field num 0, size 4, developer index 0
		0x01, // data, local message 1
	})
	if err := binary.Write(&data, binary.LittleEndian, latSemi); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&data, binary.LittleEndian, lonSemi); err != nil {
		t.Fatal(err)
	}
	data.Write([]byte{0xde, 0xad, 0xbe, 0xef})

	// session definition + data for end_position_lat/end_position_long.
	data.Write([]byte{
		0x42,       // definition, local message 2
		0x00,       // reserved
		0x00,       // little endian
		0x12, 0x00, // global message 18 (session)
		0x02,             // two fields
		0x26, 0x04, 0x85, // end_position_lat, field 38, sint32
		0x27, 0x04, 0x85, // end_position_long, field 39, sint32
		0x02, // data, local message 2
	})
	if err := binary.Write(&data, binary.LittleEndian, latSemi); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&data, binary.LittleEndian, lonSemi); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	out.WriteByte(12)
	out.WriteByte(0x10)
	_ = binary.Write(&out, binary.LittleEndian, uint16(2115))
	_ = binary.Write(&out, binary.LittleEndian, uint32(data.Len()))
	out.WriteString(".FIT")
	out.Write(data.Bytes())

	crc := dyncrc16.Checksum(out.Bytes())
	_ = binary.Write(&out, binary.LittleEndian, crc)
	return out.Bytes()
}

func wgs84ToGCJ02ForTest(lat, lon float64) (float64, float64) {
	if outOfChina(lat, lon) {
		return lat, lon
	}
	dLat, dLon := gcjDelta(lat, lon)
	return lat + dLat, lon + dLon
}

func assertNearMeters(t *testing.T, name string, wantLat, wantLon, gotLat, gotLon, tolerance float64) {
	t.Helper()
	d := distanceMetersForTest(wantLat, wantLon, gotLat, gotLon)
	if d > tolerance {
		t.Fatalf("%s = %.8f, %.8f, want %.8f, %.8f within %.2fm; got %.2fm", name, gotLat, gotLon, wantLat, wantLon, tolerance, d)
	}
}

func distanceMetersForTest(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0
	rad := func(v float64) float64 { return v * math.Pi / 180.0 }
	dLat := rad(lat2 - lat1)
	dLon := rad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusMeters * math.Asin(math.Sqrt(a))
}
