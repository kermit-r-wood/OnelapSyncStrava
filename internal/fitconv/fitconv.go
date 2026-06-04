// Package fitconv optionally converts FIT coordinates from the Chinese GCJ-02
// coordinate system into the international WGS-84 coordinate system.
package fitconv

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/tormoder/fit/dyncrc16"
)

const (
	gcjA  = 6378245.0
	gcjEE = 0.00669342162296594323

	fitHeaderSizeNoCRC = 12
	fitHeaderSizeCRC   = 14
	fitFileCRCSize     = 2
	fitDataType        = ".FIT"

	fitCompressedHeaderMask       = 0x80
	fitCompressedLocalMesgNumMask = 0x60
	fitMesgDefinitionMask         = 0x40
	fitDevDataMask                = 0x20
	fitLocalMesgNumMask           = 0x0f

	fitLittleEndian = 0
	fitBigEndian    = 1

	fitBaseSint32      = 0x85
	fitBaseSint32Index = 0x05
	fitSint32Invalid   = int32(0x7fffffff)

	fitMesgNumSession = 18
	fitMesgNumLap     = 19
	fitMesgNumRecord  = 20
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

type fitFieldDef struct {
	num      byte
	size     byte
	baseType byte
	offset   int
}

type fitDefinition struct {
	globalMsgNum uint16
	byteOrder    binary.ByteOrder
	fields       []fitFieldDef
	dataSize     int
}

type coordinateFieldPair struct {
	lat byte
	lon byte
}

var coordinatePairsByMessage = map[uint16][]coordinateFieldPair{
	fitMesgNumRecord: {
		{lat: 0, lon: 1},
	},
	fitMesgNumLap: {
		{lat: 3, lon: 4},
		{lat: 5, lon: 6},
	},
	fitMesgNumSession: {
		{lat: 3, lon: 4},
		{lat: 29, lon: 30},
		{lat: 31, lon: 32},
		{lat: 38, lon: 39},
	},
}

// ConvertFile reads the FIT file at path, converts GPS coordinate fields from
// GCJ-02 to WGS-84, and writes the result back to the same path. The converter
// patches only the existing coordinate bytes and file CRC, preserving message
// ordering, unknown fields, and developer data.
func ConvertFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read fit file: %w", err)
	}

	changed, err := convertFITBytes(data)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write converted fit file: %w", err)
	}
	return nil
}

func convertFITBytes(buf []byte) (bool, error) {
	_, dataStart, dataEnd, crcOffset, err := fitDataBounds(buf)
	if err != nil {
		return false, err
	}

	definitions := make([]*fitDefinition, 16)
	changed := false
	for pos := dataStart; pos < dataEnd; {
		recordHeader := buf[pos]
		pos++

		switch {
		case recordHeader&fitCompressedHeaderMask == fitCompressedHeaderMask:
			localMsgNum := (recordHeader & fitCompressedLocalMesgNumMask) >> 5
			def := definitions[localMsgNum]
			if def == nil {
				return false, fmt.Errorf("decode fit file: missing definition for local message %d", localMsgNum)
			}
			if pos+def.dataSize > dataEnd {
				return false, fmt.Errorf("decode fit file: data message exceeds declared data size")
			}
			if patchDataMessage(buf[pos:pos+def.dataSize], def) {
				changed = true
			}
			pos += def.dataSize

		case recordHeader&fitMesgDefinitionMask == fitMesgDefinitionMask:
			def, next, err := parseDefinition(buf, pos, dataEnd, recordHeader)
			if err != nil {
				return false, err
			}
			definitions[recordHeader&fitLocalMesgNumMask] = def
			pos = next

		default:
			localMsgNum := recordHeader & fitLocalMesgNumMask
			def := definitions[localMsgNum]
			if def == nil {
				return false, fmt.Errorf("decode fit file: missing definition for local message %d", localMsgNum)
			}
			if pos+def.dataSize > dataEnd {
				return false, fmt.Errorf("decode fit file: data message exceeds declared data size")
			}
			if patchDataMessage(buf[pos:pos+def.dataSize], def) {
				changed = true
			}
			pos += def.dataSize
		}
	}

	if changed {
		binary.LittleEndian.PutUint16(buf[crcOffset:crcOffset+fitFileCRCSize], dyncrc16.Checksum(buf[:crcOffset]))
	}
	return changed, nil
}

func fitDataBounds(buf []byte) (headerSize, dataStart, dataEnd, crcOffset int, err error) {
	if len(buf) < fitHeaderSizeNoCRC+fitFileCRCSize {
		return 0, 0, 0, 0, fmt.Errorf("decode fit file: file too small")
	}
	headerSize = int(buf[0])
	if headerSize != fitHeaderSizeNoCRC && headerSize != fitHeaderSizeCRC {
		return 0, 0, 0, 0, fmt.Errorf("decode fit file: illegal header size %d", headerSize)
	}
	if len(buf) < headerSize+fitFileCRCSize {
		return 0, 0, 0, 0, fmt.Errorf("decode fit file: truncated header")
	}
	dataSize := int(binary.LittleEndian.Uint32(buf[4:8]))
	if string(buf[8:12]) != fitDataType {
		return 0, 0, 0, 0, fmt.Errorf("decode fit file: header data type was not %q", fitDataType)
	}
	dataStart = headerSize
	dataEnd = dataStart + dataSize
	crcOffset = dataEnd
	if crcOffset+fitFileCRCSize > len(buf) {
		return 0, 0, 0, 0, fmt.Errorf("decode fit file: truncated data or CRC")
	}
	return headerSize, dataStart, dataEnd, crcOffset, nil
}

func parseDefinition(buf []byte, pos, dataEnd int, recordHeader byte) (*fitDefinition, int, error) {
	if pos+5 > dataEnd {
		return nil, pos, fmt.Errorf("decode fit file: truncated definition message")
	}

	pos++ // reserved
	arch := buf[pos]
	pos++

	var order binary.ByteOrder
	switch arch {
	case fitLittleEndian:
		order = binary.LittleEndian
	case fitBigEndian:
		order = binary.BigEndian
	default:
		return nil, pos, fmt.Errorf("decode fit file: unknown architecture %#x", arch)
	}

	globalMsgNum := order.Uint16(buf[pos : pos+2])
	pos += 2
	fieldCount := int(buf[pos])
	pos++
	if pos+fieldCount*3 > dataEnd {
		return nil, pos, fmt.Errorf("decode fit file: truncated field definitions")
	}

	fields := make([]fitFieldDef, 0, fieldCount)
	dataOffset := 0
	for i := 0; i < fieldCount; i++ {
		field := fitFieldDef{
			num:      buf[pos],
			size:     buf[pos+1],
			baseType: buf[pos+2],
			offset:   dataOffset,
		}
		pos += 3
		fields = append(fields, field)
		dataOffset += int(field.size)
	}

	if recordHeader&fitDevDataMask == fitDevDataMask {
		if pos >= dataEnd {
			return nil, pos, fmt.Errorf("decode fit file: truncated developer field count")
		}
		devFieldCount := int(buf[pos])
		pos++
		if pos+devFieldCount*3 > dataEnd {
			return nil, pos, fmt.Errorf("decode fit file: truncated developer field definitions")
		}
		for i := 0; i < devFieldCount; i++ {
			dataOffset += int(buf[pos+1])
			pos += 3
		}
	}

	return &fitDefinition{
		globalMsgNum: globalMsgNum,
		byteOrder:    order,
		fields:       fields,
		dataSize:     dataOffset,
	}, pos, nil
}

func patchDataMessage(data []byte, def *fitDefinition) bool {
	pairs := coordinatePairsByMessage[def.globalMsgNum]
	if len(pairs) == 0 {
		return false
	}

	changed := false
	for _, pair := range pairs {
		latField, latOK := def.field(pair.lat)
		lonField, lonOK := def.field(pair.lon)
		if !latOK || !lonOK || !latField.isSint32() || !lonField.isSint32() {
			continue
		}
		if latField.offset+4 > len(data) || lonField.offset+4 > len(data) {
			continue
		}

		latSemi := int32(def.byteOrder.Uint32(data[latField.offset : latField.offset+4]))
		lonSemi := int32(def.byteOrder.Uint32(data[lonField.offset : lonField.offset+4]))
		if latSemi == fitSint32Invalid || lonSemi == fitSint32Invalid {
			continue
		}

		newLat, newLon := gcj02ToWGS84(semicirclesToDegrees(latSemi), semicirclesToDegrees(lonSemi))
		newLatSemi, latValid := degreesToLatitudeSemicircles(newLat)
		newLonSemi, lonValid := degreesToLongitudeSemicircles(newLon)
		if !latValid || !lonValid || (newLatSemi == latSemi && newLonSemi == lonSemi) {
			continue
		}

		def.byteOrder.PutUint32(data[latField.offset:latField.offset+4], uint32(newLatSemi))
		def.byteOrder.PutUint32(data[lonField.offset:lonField.offset+4], uint32(newLonSemi))
		changed = true
	}
	return changed
}

func (d *fitDefinition) field(num byte) (fitFieldDef, bool) {
	for _, field := range d.fields {
		if field.num == num {
			return field, true
		}
	}
	return fitFieldDef{}, false
}

func (f fitFieldDef) isSint32() bool {
	return f.size == 4 && (f.baseType == fitBaseSint32 || f.baseType&0x1f == fitBaseSint32Index)
}

func semicirclesToDegrees(semicircles int32) float64 {
	return float64(semicircles) * 180.0 / math.Pow(2, 31)
}

func degreesToLatitudeSemicircles(degrees float64) (int32, bool) {
	if degrees >= 90 || degrees <= -90 {
		return fitSint32Invalid, false
	}
	return int32(degrees * math.Pow(2, 31) / 180.0), true
}

func degreesToLongitudeSemicircles(degrees float64) (int32, bool) {
	if degrees >= 180 || degrees <= -180 {
		return fitSint32Invalid, false
	}
	return int32(degrees * math.Pow(2, 31) / 180.0), true
}
