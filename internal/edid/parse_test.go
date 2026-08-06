package edid

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestParseDellIdentity(t *testing.T) {
	raw := syntheticEDID(0x10ac, 0x4308, 808792652, "DELL U4025QW", "GPND734", 1)
	info, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if info.Manufacturer != "DEL" || info.ProductCode != 0x4308 || info.NumericSerial != 808792652 {
		t.Fatalf("unexpected identity: %+v", info)
	}
	if info.ModelName != "DELL U4025QW" || info.TextSerial != "GPND734" {
		t.Fatalf("unexpected descriptors: %+v", info)
	}
	if err := info.ValidateExtensions(); err != nil {
		t.Fatal(err)
	}
}

func TestParseRejectsInvalidBase(t *testing.T) {
	if _, err := Parse(make([]byte, 8)); !errors.Is(err, ErrTruncated) {
		t.Fatalf("got %v", err)
	}
	raw := syntheticEDID(0x10ac, 0xd155, 1, "DELL S3423DWC", "SERIAL", 0)
	raw[20] ^= 1
	if _, err := Parse(raw); !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("got %v", err)
	}
	raw = syntheticEDID(0x10ac, 0xd155, 1, "DELL S3423DWC", "SERIAL", 0)
	raw[0] = 1
	setChecksum(raw)
	if _, err := Parse(raw); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("got %v", err)
	}
}

func TestExtensionFailureDoesNotInvalidateBase(t *testing.T) {
	raw := syntheticEDID(0x10ac, 0xd155, 1, "DELL S3423DWC", "SERIAL", 1)
	raw[BlockSize+10] ^= 1
	info, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := info.ValidateExtensions(); err == nil {
		t.Fatal("expected extension checksum failure")
	}
}

func TestStableIDDoesNotExposeSerial(t *testing.T) {
	info := Info{Manufacturer: "DEL", ProductCode: 0x4308, NumericSerial: 808792652, ModelName: "DELL U4025QW"}
	id1, state, err := StableID(info, "", true)
	if err != nil || state != monitor.IdentityHardwareStable {
		t.Fatalf("id=%q state=%q err=%v", id1, state, err)
	}
	id2, _, _ := StableID(info, "different", true)
	if id1 != id2 {
		t.Fatalf("hardware ID changed with topology: %q != %q", id1, id2)
	}
	info.NumericSerial = 0
	info.TextSerial = "unknown"
	_, state, err = StableID(info, "dcpext0", false)
	if err != nil || state != monitor.IdentityTopologyScoped {
		t.Fatalf("state=%q err=%v", state, err)
	}
}

func TestIdentityFallbacksAndDigest(t *testing.T) {
	if _, state, err := StableID(Info{}, "", false); err == nil || state != monitor.IdentityInvalidEDID {
		t.Fatalf("invalid identity state=%q err=%v", state, err)
	}
	info := Info{Manufacturer: "DEL", ProductCode: 0x4308, TextSerial: "  abc  ", ModelName: "Dell"}
	textID, state, err := StableID(info, "ignored", false)
	if err != nil || state != monitor.IdentityHardwareStable || !strings.HasPrefix(textID, "mon-") {
		t.Fatalf("text identity id=%q state=%q err=%v", textID, state, err)
	}
	info.TextSerial = "UNKNOWN"
	if _, state, err := StableID(info, "", false); !errors.Is(err, monitor.ErrAmbiguousMonitor) || state != monitor.IdentityAmbiguous {
		t.Fatalf("ambiguous state=%q err=%v", state, err)
	}
	if got := SHA256([]byte("test")); got != "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" {
		t.Fatalf("unexpected SHA-256 %s", got)
	}
}

func TestParserHandlesInvalidManufacturerAndTruncatedExtension(t *testing.T) {
	raw := syntheticEDID(0, 1, 1, "Model", "Serial", 1)
	info, err := Parse(raw[:BlockSize])
	if err != nil {
		t.Fatal(err)
	}
	if info.Manufacturer != "" {
		t.Fatalf("invalid manufacturer decoded as %q", info.Manufacturer)
	}
	if err := info.ValidateExtensions(); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("got %v", err)
	}
	if ChecksumValid(make([]byte, BlockSize-1)) {
		t.Fatal("short checksum block accepted")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(syntheticEDID(0x10ac, 0x4308, 1, "DELL U4025QW", "SERIAL", 0))
	f.Fuzz(func(t *testing.T, raw []byte) {
		info, err := Parse(raw)
		if err == nil {
			_ = info.ValidateExtensions()
		}
	})
}

func syntheticEDID(vendor, product uint16, serial uint32, model, textSerial string, extensions byte) []byte {
	raw := make([]byte, BlockSize*(int(extensions)+1))
	copy(raw, header)
	binary.BigEndian.PutUint16(raw[8:10], vendor)
	binary.LittleEndian.PutUint16(raw[10:12], product)
	binary.LittleEndian.PutUint32(raw[12:16], serial)
	raw[18], raw[19], raw[126] = 1, 4, extensions
	putDescriptor(raw[54:72], 0xfc, model)
	putDescriptor(raw[72:90], 0xff, textSerial)
	setChecksum(raw[:BlockSize])
	for i := 1; i <= int(extensions); i++ {
		block := raw[i*BlockSize : (i+1)*BlockSize]
		block[0] = 0x02
		setChecksum(block)
	}
	return raw
}

func putDescriptor(dst []byte, tag byte, value string) {
	dst[3] = tag
	copy(dst[5:], value)
}

func setChecksum(block []byte) {
	block[len(block)-1] = 0
	var sum byte
	for _, b := range block {
		sum += b
	}
	block[len(block)-1] = -sum
}
