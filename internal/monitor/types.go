package monitor

import (
	"context"
	"errors"
	"time"
)

type Generation uint64

type Snapshot struct {
	Generation Generation   `json:"generation"`
	Monitors   []Descriptor `json:"monitors"`
}

type MonitorRef struct {
	ID         string     `json:"id"`
	Generation Generation `json:"generation"`
}

type IdentityState string

const (
	IdentityHardwareStable IdentityState = "hardware-stable"
	IdentityTopologyScoped IdentityState = "topology-scoped"
	IdentityAmbiguous      IdentityState = "ambiguous"
	IdentityInvalidEDID    IdentityState = "invalid-edid"
)

type SupportState string

const (
	SupportSupported           SupportState = "supported"
	SupportUserQualified       SupportState = "user-qualified"
	SupportBackendExperimental SupportState = "backend-experimental"
	SupportUnprofiled          SupportState = "unprofiled"
	SupportAmbiguous           SupportState = "ambiguous"
	SupportEndpointUnavailable SupportState = "endpoint-unavailable"
	SupportPermissionDenied    SupportState = "permission-denied"
)

type Descriptor struct {
	ID            string        `json:"id"`
	Manufacturer  string        `json:"manufacturer"`
	Model         string        `json:"model"`
	Serial        string        `json:"serial,omitempty"`
	ProductCode   uint16        `json:"product_code"`
	Connector     string        `json:"connector"`
	EDID          []byte        `json:"-"`
	EDIDSHA256    string        `json:"edid_sha256"`
	ProfileName   string        `json:"profile_name,omitempty"`
	IdentityState IdentityState `json:"identity_state"`
	SupportState  SupportState  `json:"support_state"`
	BackendName   string        `json:"backend_name"`
}

type Input string

const (
	InputDisplayPort1 Input = "displayport-1"
	InputDisplayPort2 Input = "displayport-2"
	InputHDMI1        Input = "hdmi-1"
	InputHDMI2        Input = "hdmi-2"
	InputUSBC1        Input = "usb-c-1"
	InputUSBC2        Input = "usb-c-2"
	InputThunderbolt1 Input = "thunderbolt-1"
)

type InputValue struct {
	Logical Input  `json:"logical"`
	Raw     uint16 `json:"raw"`
}

type InputMappingSource string

const (
	InputMappingProfile           InputMappingSource = "profile"
	InputMappingUserQualification InputMappingSource = "user-qualification"
	InputMappingMCCSStandard      InputMappingSource = "mccs-standard"
	InputMappingUnmapped          InputMappingSource = "unmapped"
)

// InputCandidate is a value advertised by the monitor's DDC/CI capability
// string. Capability data is advisory: WriteQualified is true only when the
// embedded support matrix or an exact, persisted user qualification separately
// authorises this logical input.
type InputCandidate struct {
	Raw            uint16             `json:"raw"`
	Logical        Input              `json:"logical,omitempty"`
	MappingSource  InputMappingSource `json:"mapping_source"`
	WriteQualified bool               `json:"write_qualified"`
}

type CapabilityReport struct {
	Raw                   string           `json:"raw"`
	InputSourceAdvertised bool             `json:"input_source_advertised"`
	Inputs                []InputCandidate `json:"inputs"`
	Advisory              bool             `json:"advisory"`
}

type VerificationResult string

const (
	VerificationUnknown            VerificationResult = "unknown"
	VerificationWriteAccepted      VerificationResult = "write-accepted"
	VerificationConfirmed          VerificationResult = "confirmed"
	VerificationAssumedSuccess     VerificationResult = "assumed-success"
	VerificationDisplayPathChanged VerificationResult = "display-path-changed"
)

type VerificationIssueCategory string

const (
	VerificationIssueNoReply             VerificationIssueCategory = "no-reply"
	VerificationIssueMalformedReply      VerificationIssueCategory = "malformed-reply"
	VerificationIssueChecksum            VerificationIssueCategory = "checksum"
	VerificationIssueTimeout             VerificationIssueCategory = "timeout"
	VerificationIssueDisconnected        VerificationIssueCategory = "disconnected"
	VerificationIssueEndpointUnavailable VerificationIssueCategory = "endpoint-unavailable"
	VerificationIssueReadUnsupported     VerificationIssueCategory = "read-unsupported"
)

type VerificationIssue struct {
	Category    VerificationIssueCategory `json:"category"`
	Detail      string                    `json:"detail,omitempty"`
	RawReplyHex string                    `json:"raw_reply_hex,omitempty"`
}

type SwitchResult struct {
	Monitor              MonitorRef         `json:"monitor"`
	Target               InputValue         `json:"target"`
	Verification         VerificationResult `json:"verification"`
	Observed             *InputValue        `json:"observed,omitempty"`
	Attempts             int                `json:"attempts"`
	VerificationAttempts int                `json:"verification_attempts,omitempty"`
	VerificationIssue    *VerificationIssue `json:"verification_issue,omitempty"`
	Duration             time.Duration      `json:"duration"`
}

type Backend interface {
	Name() string
	Enumerate(context.Context) (Snapshot, error)
	Open(context.Context, MonitorRef) (RawInputSession, error)
}

type RawInputSession interface {
	GetInputRaw(context.Context) (uint16, error)
	SetInputRaw(context.Context, uint16) error
	Close() error
}

// CapabilitySession is an optional, read-only extension implemented by
// backends that can retrieve a DDC/CI capabilities string.
type CapabilitySession interface {
	Capabilities(context.Context) (string, error)
}

var (
	ErrNoMonitors               = errors.New("no external monitors found")
	ErrMonitorNotFound          = errors.New("monitor not found")
	ErrAmbiguousMonitor         = errors.New("monitor identity is ambiguous")
	ErrUnsupportedMonitor       = errors.New("monitor has no verified input profile")
	ErrUnqualifiedSlice         = errors.New("platform and monitor combination is not qualified")
	ErrQualificationUnavailable = errors.New("user qualification persistence is unavailable")
	ErrQualificationIncomplete  = errors.New("hardware qualification evidence is incomplete")
	ErrUserQualificationStale   = errors.New("saved user qualification no longer matches advertised capabilities")
	ErrUnknownInput             = errors.New("input is not defined by the monitor profile")
	ErrUnsupportedConfigSchema  = errors.New("configuration schema is unsupported")
	ErrPermissionDenied         = errors.New("permission denied")
	ErrBackendUnavailable       = errors.New("DDC backend unavailable")
	ErrEndpointNotFound         = errors.New("DDC endpoint not found")
	ErrDDCDisabled              = errors.New("DDC/CI may be disabled")
	ErrReadUnsupported          = errors.New("reading input source is unsupported")
	ErrCapabilitiesUnsupported  = errors.New("DDC capability retrieval is unsupported")
	ErrWriteUnsupported         = errors.New("writing input source is unsupported")
	ErrTransactionNACK          = errors.New("DDC transaction was not acknowledged")
	ErrTransactionTimeout       = errors.New("DDC transaction timed out")
	ErrMalformedReply           = errors.New("malformed DDC reply")
	ErrChecksum                 = errors.New("DDC checksum failure")
	ErrDisconnected             = errors.New("monitor disconnected")
	ErrStaleEnumeration         = errors.New("monitor enumeration is stale")
	ErrPrivateSymbolUnavailable = errors.New("required private macOS symbol unavailable")
)
