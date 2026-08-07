# Hardware qualification

Hardware qualification is explicit, identity-scoped, and never cycles possible
input values. Keep a working monitor OSD recovery path available before any
write.

## 1. Read-only gate

```sh
./build-support/script/validate.sh hardware-read \
  --monitor-id <stable-id> \
  --expected-model <exact-model> \
  --expected-edid-sha256 <sha256>
```

This enumerates the exact display, confirms model and EDID hash, reads VCP
`0x60`, retrieves the capability string when the monitor supports the DDC/CI
capabilities request, and writes artifacts under `test-artifacts/`. It performs
no write.

To inspect advertised input values directly:

```sh
xdispddcswtchr input detect --monitor <stable-id>
xdispddcswtchr input detect --monitor <stable-id> --json
```

This avoids manual transcription for values present in `vcp(60(...))`.
Standard `0x0F`/`0x10`/`0x11`/`0x12` values are labelled as DisplayPort 1/2 and
HDMI 1/2. A matching profile labels known vendor-specific values first. Unknown
values remain visible as raw candidates and require physical qualification;
the detection result cannot enable a write.

## 2. Run the read-only input preflight

The qualification binary reports the current raw VCP value, the physical OSD
choices for the exact Dell model, and which fields still require operator
confirmation:

```sh
go build -tags qualification -o test-artifacts/xdispddcswtchr-qualify ./cmd/xdispddcswtchr-qualify
test-artifacts/xdispddcswtchr-qualify input preflight \
  --monitor <stable-id> \
  --expected-model <exact-model> \
  --expected-edid-sha256 <sha256>
```

Preflight performs no write. DDC/CI supplies a raw VCP value, not the monitor's
visible label. The native endpoint can identify the display but cannot reliably
prove whether an external dock, adapter, or KVM is in the video path.

## 3. Capture the current OSD label

Build the non-release qualification command and record the label shown by the
monitor OSD:

```sh
go build -tags qualification -o test-artifacts/xdispddcswtchr-qualify ./cmd/xdispddcswtchr-qualify
test-artifacts/xdispddcswtchr-qualify input capture \
  --monitor <stable-id> \
  --expected-model <exact-model> \
  --expected-edid-sha256 <sha256> \
  --label <logical-input> \
  --osd-label '<exact visible OSD label>'
```

Do not infer the label from a generic MCCS table. The operator must confirm the
current monitor OSD label. For U4025QW the physical choices are `Thunderbolt
(140W)`, `DP`, and `HDMI`. For S3423DWC they are `USB-C`, `HDMI 1`, and `HDMI
2`. If the U4025QW input was renamed, record the exact visible custom label but
still select the logical input for the physical connector.

Before a write, trace the cable and record either `direct` or the manufacturer
and model of every dock, adapter, or KVM in the video path. Confirm that the
target input has a live source, and test the physical monitor-button/joystick
path back to the current input. That physical OSD path is the preferred recovery
method because switching can make DDC unreachable from the current host.

Model input choices were checked against Dell's current support catalogue and
the vendor user guides on 2026-08-07:

- [Dell U4025QW User's Guide](https://dl.dell.com/content/manual7178794-dell-ultrasharp-40-curved-thunderbolt-hub-monitor-u4025qw-user-s-guide.pdf?language=en-us)
- [Dell S3423DWC User's Guide](https://dl.dell.com/content/manual75890860-dell-s3423dwc-monitor-user-s-guide.pdf?language=en-us)

## 4. Tracked capability mappings

The operator-provided Windows capability enumeration is tracked at
`qualification/evidence/2026-08-07-user-provided-vcp-capabilities.json` and
populates these model profiles:

| Profile | Logical input | Raw VCP `0x60` |
|---|---|---:|
| Dell U4025QW | `thunderbolt-1` | `0x19` |
| Dell U4025QW | `displayport-1` | `0x0F` |
| Dell U4025QW | `hdmi-1` | `0x11` |
| Dell S3423DWC | `usb-c-1` | `0x1B` |
| Dell S3423DWC | `hdmi-1` | `0x11` |
| Dell S3423DWC | `hdmi-2` | `0x12` |
| LG UltraGear OLED 45GX950A-B | `displayport-1` | `0x0F` |
| LG UltraGear OLED 45GX950A-B | `displayport-2` | `0x10` |
| LG UltraGear OLED 45GX950A-B | `hdmi-1` | `0x11` |
| LG UltraGear OLED 45GX950A-B | `hdmi-2` | `0x12` |

For U4025QW, `0x19` is assigned to `thunderbolt-1` by elimination from the
model's documented Thunderbolt, DP, and HDMI inputs; confirm the visible OSD
label during write qualification. For the LG, the supplied capability label
`DisplayPort-2` is preserved, but current LG specifications list one physical
DisplayPort plus USB-C. Do not silently relabel `0x10` as USB-C without physical
evidence.

Capability enumeration qualifies the profile mapping data, not a complete
release-supported platform slice. `qualification/support-matrix.json` remains
empty until write/readback, topology, timing, and recovery evidence is captured.
The normal TUI can separately persist an exact local `user-qualified` decision
after the operator reviews every advertised value. That enables only the
accepted mappings on that identity/endpoint, is revalidated before each write,
and is never evidence for a release support claim.

This policy is deliberate. The current ddcutil documentation warns that a
monitor's capability string is informational and is often incorrect, including
known cases where the advertised `0x60` values differ from the values actually
accepted by the display. USB-C has no standard MCCS `0x60` value, so it cannot
be labelled generically without monitor-specific evidence. See the [current
capability reliability notes](https://www.ddcutil.com/faq/).

Current LG product information and its 2026-02-20 manual are available from
[LG's official support page](https://www.lg.com/us/support/product/lg-45GX950A-B).

## 5. Single guarded switch

Only after the source and target mappings are evidence-backed:

```sh
./build-support/script/validate.sh hardware-write \
  --monitor-id <stable-id> \
  --expected-model <exact-model> \
  --expected-edid-sha256 <sha256> \
  --source-input <logical-input> \
  --target-input <logical-input> \
  --acknowledge-switch-away \
  --recovery-method '<operator recovery procedure>'
```

The command verifies the declared source raw value before issuing one logical
target write. Ambiguous timeouts are not automatically retried. A readback may
confirm the result or report a display-path change; the operator must also
confirm the visible target and exercise the recovery method.

If Set VCP succeeds but every configured read-back is malformed, fails its
checksum, times out, is NACKed, or explicitly reports read unsupported, the
normal application reports `assumed-success`. This is a usable application
outcome, not hardware-qualification evidence. The qualification command writes
a schema-version `2` partial JSON record containing `verification_attempts`, a
typed `verification_issue`, and `raw_reply_hex` when available, then exits with
verification code `6`. It issues no repeat Set operation. A valid read-back
that differs from the target is `unknown` and is also incomplete evidence.

On Apple Silicon, the native boundary accepts either a standard checksummed
11-byte Get VCP response or the exact compact CoreDisplay representation: the
eight bytes beginning at the DDC result field followed by three zero-filled
buffer bytes. The backend reconstructs the standard `6e 88 02` header and
passes the result through the unchanged strict parser and checksum validation.
It does not scan arbitrary offsets or accept other shortened forms.

Sanitize serials and raw EDID before tracking evidence. Hash the immutable
evidence file and add a support record only for the exact tested OS build,
application commit, backend, EDID hash, connector, and input set. Corrections
create a new evidence file; do not rewrite published evidence.
