package verification

import (
	"context"
	"errors"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestAssumableReadback(t *testing.T) {
	for _, err := range []error{
		monitor.ErrTransactionNACK,
		monitor.ErrTransactionTimeout,
		monitor.ErrMalformedReply,
		monitor.ErrChecksum,
		monitor.ErrReadUnsupported,
	} {
		if !AssumableReadback(err) {
			t.Errorf("expected %v to be assumable", err)
		}
	}
	for _, err := range []error{
		context.Canceled,
		monitor.ErrPermissionDenied,
		monitor.ErrBackendUnavailable,
		monitor.ErrDisconnected,
		monitor.ErrEndpointNotFound,
	} {
		if AssumableReadback(err) {
			t.Errorf("expected %v not to be assumable", err)
		}
	}
}

func TestIssueIncludesTypedCategoryAndRawReply(t *testing.T) {
	_, err := ddc.ParseGetVCPReply([]byte{0x00, 0x60, 0x00}, ddc.VCPInputSource)
	if !errors.Is(err, monitor.ErrMalformedReply) {
		t.Fatalf("expected malformed reply, got %v", err)
	}
	issue := Issue(err)
	if issue.Category != monitor.VerificationIssueMalformedReply {
		t.Fatalf("category = %q", issue.Category)
	}
	if issue.RawReplyHex != "006000" {
		t.Fatalf("raw reply = %q", issue.RawReplyHex)
	}
	if issue.Detail == "" {
		t.Fatal("expected detail")
	}
}

func TestIssueClassifiesDisplayPathErrors(t *testing.T) {
	if got := Issue(monitor.ErrDisconnected).Category; got != monitor.VerificationIssueDisconnected {
		t.Fatalf("disconnected category = %q", got)
	}
	if got := Issue(monitor.ErrEndpointNotFound).Category; got != monitor.VerificationIssueEndpointUnavailable {
		t.Fatalf("endpoint category = %q", got)
	}
}
