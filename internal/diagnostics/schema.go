package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/edid"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/platform"
)

const SchemaVersion = 1

type Application struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type Backend struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
}

type Support struct {
	State               monitor.SupportState `json:"state"`
	QualificationRecord string               `json:"qualification_record,omitempty"`
}

type Redaction struct {
	Enabled bool   `json:"enabled"`
	Method  string `json:"method"`
}

type Monitor struct {
	ID            string                `json:"id"`
	Manufacturer  string                `json:"manufacturer"`
	ProductCode   uint16                `json:"product_code"`
	Serial        string                `json:"serial,omitempty"`
	ModelName     string                `json:"model_name"`
	Connector     string                `json:"connector"`
	IdentityState monitor.IdentityState `json:"identity_state"`
}

type EDID struct {
	Raw           string `json:"raw,omitempty"`
	SHA256        string `json:"sha256"`
	ChecksumValid bool   `json:"checksum_valid"`
}

type InputSource struct {
	VCPCode        string                   `json:"vcp_code"`
	ReadSupported  bool                     `json:"read_supported"`
	CurrentRaw     *uint16                  `json:"current_raw,omitempty"`
	CurrentLogical monitor.Input            `json:"current_logical,omitempty"`
	ProfileValues  []monitor.InputValue     `json:"profile_values,omitempty"`
	DetectedValues []monitor.InputCandidate `json:"detected_values,omitempty"`
}

type Timings struct {
	ReadMilliseconds int64 `json:"read_ms,omitempty"`
}

type Bundle struct {
	SchemaVersion   int           `json:"schema_version"`
	GeneratedAt     time.Time     `json:"generated_at"`
	Application     Application   `json:"application"`
	OS              platform.Info `json:"os"`
	Backend         Backend       `json:"backend"`
	Support         Support       `json:"support"`
	Redaction       Redaction     `json:"redaction"`
	Monitor         Monitor       `json:"monitor"`
	EDID            EDID          `json:"edid"`
	InputSource     InputSource   `json:"input_source"`
	CapabilitiesRaw string        `json:"capabilities_raw,omitempty"`
	Timings         Timings       `json:"timings"`
	Errors          []string      `json:"errors"`
}

type BuildOptions struct {
	Now                 time.Time
	Version             string
	Commit              string
	Platform            platform.Info
	Descriptor          monitor.Descriptor
	Inputs              []monitor.InputValue
	Current             *monitor.InputValue
	ReadDuration        time.Duration
	ReadError           error
	Redact              bool
	QualificationRecord string
	Capabilities        *monitor.CapabilityReport
	CapabilityError     error
}

func Build(options BuildOptions) Bundle {
	descriptor := options.Descriptor
	serial := descriptor.Serial
	rawEDID := hex.EncodeToString(descriptor.EDID)
	method := "none"
	if options.Redact {
		serial = redact(serial)
		rawEDID = ""
		method = "sha256-truncated; raw EDID omitted"
	}
	bundle := Bundle{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   options.Now.UTC(),
		Application:   Application{Version: options.Version, Commit: options.Commit},
		OS:            options.Platform,
		Backend:       Backend{Name: descriptor.BackendName, Transport: "i2c-ddc-ci", Endpoint: descriptor.Connector},
		Support:       Support{State: descriptor.SupportState, QualificationRecord: options.QualificationRecord},
		Redaction:     Redaction{Enabled: options.Redact, Method: method},
		Monitor: Monitor{
			ID: descriptor.ID, Manufacturer: descriptor.Manufacturer, ProductCode: descriptor.ProductCode,
			Serial: serial, ModelName: descriptor.Model, Connector: descriptor.Connector, IdentityState: descriptor.IdentityState,
		},
		EDID:        EDID{Raw: rawEDID, SHA256: descriptor.EDIDSHA256, ChecksumValid: len(descriptor.EDID) >= edid.BlockSize && edid.ChecksumValid(descriptor.EDID[:edid.BlockSize])},
		InputSource: InputSource{VCPCode: "0x60", ReadSupported: options.ReadError == nil, ProfileValues: options.Inputs},
		Timings:     Timings{ReadMilliseconds: options.ReadDuration.Milliseconds()},
		Errors:      []string{},
	}
	if options.Current != nil {
		value := options.Current.Raw
		bundle.InputSource.CurrentRaw = &value
		bundle.InputSource.CurrentLogical = options.Current.Logical
	}
	if options.ReadError != nil {
		bundle.Errors = append(bundle.Errors, options.ReadError.Error())
	}
	if options.Capabilities != nil {
		bundle.CapabilitiesRaw = options.Capabilities.Raw
		bundle.InputSource.DetectedValues = append([]monitor.InputCandidate(nil), options.Capabilities.Inputs...)
	}
	if options.CapabilityError != nil {
		bundle.Errors = append(bundle.Errors, options.CapabilityError.Error())
	}
	return bundle
}

func Marshal(bundle Bundle) ([]byte, error) {
	return json.MarshalIndent(bundle, "", "  ")
}

func redact(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:8])
}
