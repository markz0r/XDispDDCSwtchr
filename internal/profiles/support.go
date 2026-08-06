package profiles

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

//go:embed support-matrix.json
var supportMatrixJSON []byte

type SupportMatrix struct {
	SchemaVersion int             `json:"schema_version"`
	Records       []SupportRecord `json:"records"`
}

type SupportRecord struct {
	ID                  string           `json:"id"`
	OS                  string           `json:"os"`
	Architecture        string           `json:"architecture"`
	OSVersion           string           `json:"os_version"`
	OSBuild             string           `json:"os_build"`
	Backend             string           `json:"backend"`
	ApplicationCommit   string           `json:"application_commit"`
	Profile             string           `json:"profile"`
	EDIDSHA256          string           `json:"edid_sha256"`
	Connector           string           `json:"connector"`
	Inputs              []string         `json:"inputs"`
	Verification        VerificationMode `json:"verification"`
	QualificationRecord string           `json:"qualification_record"`
	EvidenceSHA256      string           `json:"evidence_sha256"`
}

type Platform struct {
	OS                string
	Architecture      string
	Version           string
	Build             string
	ApplicationCommit string
}

func LoadSupportMatrix() (*SupportMatrix, error) {
	return ParseSupportMatrix(supportMatrixJSON)
}

func ParseSupportMatrix(data []byte) (*SupportMatrix, error) {
	var matrix SupportMatrix
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		return nil, fmt.Errorf("parse embedded support matrix: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse embedded support matrix: trailing JSON value")
		}
		return nil, fmt.Errorf("parse embedded support matrix: %w", err)
	}
	if matrix.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported support matrix schema %d", matrix.SchemaVersion)
	}
	seen := make(map[string]bool, len(matrix.Records))
	for i, record := range matrix.Records {
		if strings.TrimSpace(record.ID) == "" || seen[record.ID] {
			return nil, fmt.Errorf("support record %d has an empty or duplicate id %q", i, record.ID)
		}
		seen[record.ID] = true
		if record.OS == "" || record.Architecture == "" || record.OSVersion == "" || record.OSBuild == "" ||
			record.Backend == "" || record.ApplicationCommit == "" || record.Profile == "" || record.EDIDSHA256 == "" ||
			record.Connector == "" || len(record.Inputs) == 0 || record.Verification == "" || record.QualificationRecord == "" || record.EvidenceSHA256 == "" {
			return nil, fmt.Errorf("support record %q is incomplete", record.ID)
		}
		if !validHexDigest(record.EDIDSHA256, 64) || !validHexDigest(record.EvidenceSHA256, 64) ||
			!(validHexDigest(record.ApplicationCommit, 40) || validHexDigest(record.ApplicationCommit, 64)) {
			return nil, fmt.Errorf("support record %q contains an invalid identity or evidence digest", record.ID)
		}
		cleanEvidence := path.Clean(record.QualificationRecord)
		if cleanEvidence != record.QualificationRecord || !strings.HasPrefix(cleanEvidence, "qualification/evidence/") {
			return nil, fmt.Errorf("support record %q has an unsafe qualification record path", record.ID)
		}
		if record.Verification != VerifyNone && record.Verification != VerifyImmediate && record.Verification != VerifyAfterReconnect {
			return nil, fmt.Errorf("support record %q has an invalid verification mode", record.ID)
		}
		inputs := make(map[monitor.Input]bool, len(record.Inputs))
		for _, value := range record.Inputs {
			input := monitor.Input(value)
			if !knownLogicalInput(input) || inputs[input] {
				return nil, fmt.Errorf("support record %q has an unknown or duplicate input %q", record.ID, value)
			}
			inputs[input] = true
		}
	}
	return &matrix, nil
}

func validHexDigest(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func knownLogicalInput(input monitor.Input) bool {
	switch input {
	case monitor.InputDisplayPort1, monitor.InputDisplayPort2, monitor.InputHDMI1, monitor.InputHDMI2,
		monitor.InputUSBC1, monitor.InputUSBC2, monitor.InputThunderbolt1:
		return true
	default:
		return false
	}
}

func (m *SupportMatrix) Match(platform Platform, backend, profile, edidHash, connector string) (*SupportRecord, bool) {
	for i := range m.Records {
		r := &m.Records[i]
		if r.OS == platform.OS && r.Architecture == platform.Architecture &&
			r.OSVersion == platform.Version && r.OSBuild == platform.Build &&
			r.Backend == backend && r.ApplicationCommit == platform.ApplicationCommit &&
			r.Profile == profile && r.EDIDSHA256 == edidHash &&
			r.Connector == connector && r.QualificationRecord != "" && r.EvidenceSHA256 != "" {
			return r, true
		}
	}
	return nil, false
}
