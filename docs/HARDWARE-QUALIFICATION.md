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
`0x60`, and writes artifacts under `test-artifacts/`. It performs no write.

## 2. Capture the current OSD label

Build the non-release qualification command and record the label shown by the
monitor OSD:

```sh
go build -tags qualification -o test-artifacts/xdispddcswtchr-qualify ./cmd/xdispddcswtchr-qualify
test-artifacts/xdispddcswtchr-qualify input capture \
  --monitor <stable-id> \
  --expected-model <exact-model> \
  --expected-edid-sha256 <sha256> \
  --label <logical-input>
```

Do not infer the label from a generic MCCS table. The operator must confirm the
current monitor OSD label.

## 3. Single guarded switch

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

Sanitize serials and raw EDID before tracking evidence. Hash the immutable
evidence file and add a support record only for the exact tested OS build,
application commit, backend, EDID hash, connector, and input set. Corrections
create a new evidence file; do not rewrite published evidence.
