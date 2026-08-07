# macOS native backend compatibility

Validated read-only host slice:

- macOS 26.6 (25G72)
- Apple Silicon arm64
- Xcode 26.6 (17F113)
- Dell S3423DWC and Dell U4025QW connected directly through DCP services

Discovery starts with CoreGraphics active displays. The C bridge correlates
each external display to an IOKit DCP service using the EDID manufacturer,
product, and serial tuple. The Go layer parses and validates the EDID, derives a
stable pseudonymous ID, owns enumeration generations, and opens only the
correlated registry service.

DDC access is deliberately narrow: Get/Set VCP `0x60` only. The bridge resolves
the required display-service functions at runtime and checks every symbol before
use. No static dependency is taken on a private framework path, and failure to
resolve or correlate produces a typed unavailable/endpoint error. There is no
subprocess fallback.

Get VCP parsing remains strict and checksum-validated. The backend first parses
the standard 11-byte DDC/CI response. Some Apple Silicon CoreDisplay
transactions return only the eight-byte suffix beginning at the result field
into an 11-byte zero-initialized buffer. The backend accepts that form only
when bytes 8 through 10 remain zero and byte 1 is the requested VCP code,
prepends the standard `6e 88 02` header, and runs the same parser again. It does
not perform offset scanning or relax the generic DDC parser. Rejected replies
retain the exact transport bytes for typed verification diagnostics.

The current CoreDisplay image is discovered at runtime. On the validated host it
is `/System/Library/Frameworks/CoreDisplay.framework/CoreDisplay`; older private
framework path assumptions are not embedded in the program.

Read evidence is stored in
`qualification/evidence/2026-08-06-macos-26.6-read-only.json`. This proves
feasibility only. It is not a write qualification record and does not enable a
normal production write.

An operator-observed U4025QW switch to Thunderbolt succeeded while the previous
build rejected the post-write reply as `invalid payload length 96`. This is
implementation evidence for the compact reply boundary above, but it is not a
release qualification claim until the guarded workflow captures valid
read-back or separately completes the required physical recovery evidence.

References:

- [Apple CoreGraphics display functions](https://developer.apple.com/documentation/coregraphics/display-functions)
- [Apple IOKit](https://developer.apple.com/documentation/iokit)
- [Apple Core Foundation ownership policy](https://developer.apple.com/library/archive/documentation/CoreFoundation/Conceptual/CFMemoryMgmt/Concepts/Ownership.html)
