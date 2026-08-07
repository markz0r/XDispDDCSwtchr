package verification

import (
	"encoding/hex"
	"errors"

	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

// AssumableReadback reports whether err means that a successful Set VCP
// operation could not subsequently be verified from display-provided data.
// Local cancellation, permission, backend, and configuration failures are
// intentionally excluded.
func AssumableReadback(err error) bool {
	return errors.Is(err, monitor.ErrTransactionNACK) ||
		errors.Is(err, monitor.ErrTransactionTimeout) ||
		errors.Is(err, monitor.ErrMalformedReply) ||
		errors.Is(err, monitor.ErrChecksum) ||
		errors.Is(err, monitor.ErrReadUnsupported)
}

// Issue converts a classified verification failure into a stable diagnostic
// shape. Detail remains human-readable; callers must use Category for logic.
func Issue(err error) *monitor.VerificationIssue {
	if err == nil {
		return nil
	}
	issue := &monitor.VerificationIssue{
		Category: category(err),
		Detail:   err.Error(),
	}
	if raw, ok := ddc.ReplyBytes(err); ok {
		issue.RawReplyHex = hex.EncodeToString(raw)
	}
	return issue
}

func category(err error) monitor.VerificationIssueCategory {
	switch {
	case errors.Is(err, monitor.ErrMalformedReply):
		return monitor.VerificationIssueMalformedReply
	case errors.Is(err, monitor.ErrChecksum):
		return monitor.VerificationIssueChecksum
	case errors.Is(err, monitor.ErrTransactionTimeout):
		return monitor.VerificationIssueTimeout
	case errors.Is(err, monitor.ErrDisconnected):
		return monitor.VerificationIssueDisconnected
	case errors.Is(err, monitor.ErrEndpointNotFound):
		return monitor.VerificationIssueEndpointUnavailable
	case errors.Is(err, monitor.ErrReadUnsupported):
		return monitor.VerificationIssueReadUnsupported
	default:
		return monitor.VerificationIssueNoReply
	}
}
