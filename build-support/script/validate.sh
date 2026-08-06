#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
cd "$repo_root"

mode=${1:-quick}
if (($# > 0)); then shift; fi
stamp=$(date -u +%Y%m%dT%H%M%SZ)
artifact_dir="$repo_root/test-artifacts/$stamp-$mode"
mkdir -p "$artifact_dir"

export GOTOOLCHAIN=${XDISP_GOTOOLCHAIN:-${GOTOOLCHAIN:-go1.26.5}}
export GOPATH=${XDISP_GOPATH:-"$repo_root/test-artifacts/.gopath"}
export GOCACHE=${XDISP_GOCACHE:-"$repo_root/test-artifacts/.gocache"}
export GOFLAGS=${GOFLAGS:-"-mod=readonly"}

status=failed
finish() {
    exit_code=$?
    if ((exit_code == 0)); then status=passed; fi
    printf '{"schema_version":1,"mode":"%s","status":"%s","exit_code":%d,"artifact_directory":"%s"}\n' \
        "$mode" "$status" "$exit_code" "$artifact_dir" >"$artifact_dir/summary.json"
    printf 'artifacts: %s\n' "$artifact_dir"
}
trap finish EXIT

actual_go=$(go env GOVERSION)
if [[ "$actual_go" != "go1.26.5" ]]; then
    printf 'Go toolchain %s is active; go1.26.5 is required\n' "$actual_go" >&2
    exit 1
fi

run_quick() {
    go version | tee "$artifact_dir/go-version.txt"
    go_files="$artifact_dir/go-files.txt"
    existing=()
    while IFS= read -r candidate; do
        if [[ -f "$candidate" ]]; then
            existing[${#existing[@]}]="$candidate"
            printf '%s\n' "$candidate" >>"$go_files"
        fi
    done < <(git ls-files --cached --others --exclude-standard -- '*.go')
    if ((${#existing[@]} > 0)); then
        gofmt -d "${existing[@]}" >"$artifact_dir/gofmt.diff"
    fi
    if [[ -s "$artifact_dir/gofmt.diff" ]]; then
        cat "$artifact_dir/gofmt.diff"
        return 1
    fi
    GOFLAGS= go mod tidy -diff | tee "$artifact_dir/go-mod-tidy.log"
    go mod verify | tee "$artifact_dir/go-mod-verify.log"
    go vet ./... | tee "$artifact_dir/go-vet.log"
    go test -short -shuffle=on -count=1 ./... | tee "$artifact_dir/go-test-short.log"
    go test -short -shuffle=on -count=1 -tags qualification ./cmd/xdispddcswtchr-qualify | tee "$artifact_dir/go-test-qualification.log"
}

run_full() {
    run_quick
    go test -shuffle=on -count=1 ./... | tee "$artifact_dir/go-test.log"
    go test -race -shuffle=on -count=1 ./... | tee "$artifact_dir/go-test-race.log"
    go test -count=10 ./internal/service ./internal/tui | tee "$artifact_dir/go-test-repeat.log"
    go test -covermode=atomic -coverprofile="$artifact_dir/coverage.out" ./... | tee "$artifact_dir/go-test-coverage.log"
    go tool cover -func="$artifact_dir/coverage.out" >"$artifact_dir/coverage.txt"
    for package in ddc edid profiles service; do
        profile="$artifact_dir/coverage-$package.out"
        go test -covermode=atomic -coverprofile="$profile" "./internal/$package" | tee "$artifact_dir/coverage-$package.log"
        go tool cover -func="$profile" >"$artifact_dir/coverage-$package.txt"
        percentage=$(awk '/^total:/{gsub(/%/, "", $3); print $3}' "$artifact_dir/coverage-$package.txt")
        if ! awk -v percentage="$percentage" 'BEGIN { exit !(percentage + 0 >= 90) }'; then
            printf '%s coverage %s%% is below 90%%\n' "$package" "$percentage" >&2
            return 1
        fi
    done
    common_cover_packages='./internal/cli,./internal/config,./internal/ddc,./internal/diagnostics,./internal/edid,./internal/monitor,./internal/platform,./internal/profiles,./internal/service,./internal/tui'
    go test -covermode=atomic -coverpkg="$common_cover_packages" -coverprofile="$artifact_dir/coverage-common.out" \
        ./internal/cli ./internal/config ./internal/ddc ./internal/diagnostics ./internal/edid ./internal/monitor \
        ./internal/platform ./internal/profiles ./internal/service ./internal/tui | tee "$artifact_dir/coverage-common.log"
    go tool cover -func="$artifact_dir/coverage-common.out" >"$artifact_dir/coverage-common.txt"
    common_percentage=$(awk '/^total:/{gsub(/%/, "", $3); print $3}' "$artifact_dir/coverage-common.txt")
    if ! awk -v percentage="$common_percentage" 'BEGIN { exit !(percentage + 0 >= 80) }'; then
        printf 'common package coverage %s%% is below 80%%\n' "$common_percentage" >&2
        return 1
    fi
    go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./... | tee "$artifact_dir/govulncheck.log"
}

build_binaries() {
    commit=$(git rev-parse --verify HEAD)
    go version >"$artifact_dir/go-version.txt"
    go build -trimpath -ldflags "-s -w -X main.version=0.1.0-dev -X main.commit=$commit" -o "$artifact_dir/xdispddcswtchr" ./cmd/xdispddcswtchr
    go build -trimpath -tags qualification -o "$artifact_dir/xdispddcswtchr-qualify" ./cmd/xdispddcswtchr-qualify
    go version -m "$artifact_dir/xdispddcswtchr" >"$artifact_dir/build-metadata.txt"
}

run_platform() {
    run_full
    build_binaries
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o "$artifact_dir/xdispddcswtchr-darwin-arm64-nocgo" ./cmd/xdispddcswtchr
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$artifact_dir/xdispddcswtchr-linux-amd64" ./cmd/xdispddcswtchr
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o "$artifact_dir/xdispddcswtchr-windows-amd64.exe" ./cmd/xdispddcswtchr
    "$artifact_dir/xdispddcswtchr" version >"$artifact_dir/version.txt"
    "$artifact_dir/xdispddcswtchr" help >"$artifact_dir/help.stdout" 2>"$artifact_dir/help.stderr"
    set +e
    "$artifact_dir/xdispddcswtchr" </dev/null >"$artifact_dir/non-tty.stdout" 2>"$artifact_dir/non-tty.stderr"
    non_tty_code=$?
    set -e
    if ((non_tty_code != 2)); then
        printf 'non-TTY invocation returned %d, expected 2\n' "$non_tty_code" >&2
        return 1
    fi
    printf '{"backend":"cli","hotkeys":[]}' >"$artifact_dir/legacy-config.json"
    set +e
    "$artifact_dir/xdispddcswtchr" --config "$artifact_dir/legacy-config.json" list >"$artifact_dir/legacy.stdout" 2>"$artifact_dir/legacy.stderr"
    legacy_code=$?
    set -e
    if ((legacy_code != 2)); then
        printf 'legacy configuration returned %d, expected 2\n' "$legacy_code" >&2
        return 1
    fi
    if command -v otool >/dev/null 2>&1; then
        otool -L "$artifact_dir/xdispddcswtchr" >"$artifact_dir/linked-libraries.txt"
    fi
    if strings "$artifact_dir/xdispddcswtchr" | grep -Eiq 'controlmymonitor|ddcutil|ddcctl'; then
        printf 'production binary contains a forbidden external monitor-control dependency marker\n' >&2
        return 1
    fi
}

monitor_id=
expected_model=
expected_edid=
source_input=
target_input=
acknowledge_switch=false
recovery_method=
while (($# > 0)); do
    case "$1" in
        --monitor-id) monitor_id=$2; shift 2 ;;
        --expected-model) expected_model=$2; shift 2 ;;
        --expected-edid-sha256) expected_edid=$2; shift 2 ;;
        --source-input) source_input=$2; shift 2 ;;
        --target-input) target_input=$2; shift 2 ;;
        --acknowledge-switch-away) acknowledge_switch=true; shift ;;
        --recovery-method) recovery_method=$2; shift 2 ;;
        *) printf 'unknown argument: %s\n' "$1" >&2; exit 2 ;;
    esac
done

require_hardware_identity() {
    if [[ -z "$monitor_id" || -z "$expected_model" || -z "$expected_edid" ]]; then
        printf 'hardware mode requires --monitor-id, --expected-model, and --expected-edid-sha256\n' >&2
        exit 2
    fi
}

run_hardware_read() {
    require_hardware_identity
    build_binaries
    "$artifact_dir/xdispddcswtchr" diagnose --monitor "$monitor_id" --output "$artifact_dir/read-diagnostic.json" >"$artifact_dir/diagnose.stdout"
    grep -Fq "\"id\": \"$monitor_id\"" "$artifact_dir/read-diagnostic.json"
    grep -Fq "\"model_name\": \"$expected_model\"" "$artifact_dir/read-diagnostic.json"
    grep -Fq "\"sha256\": \"$expected_edid\"" "$artifact_dir/read-diagnostic.json"
    grep -Fq '"enabled": true' "$artifact_dir/read-diagnostic.json"
}

run_hardware_write() {
    require_hardware_identity
    if [[ -z "$source_input" || -z "$target_input" || "$acknowledge_switch" != true || -z "$recovery_method" ]]; then
        printf 'hardware-write requires source, target, acknowledgement, and recovery method\n' >&2
        exit 2
    fi
    run_hardware_read
    "$artifact_dir/xdispddcswtchr-qualify" input write \
        --monitor "$monitor_id" \
        --expected-model "$expected_model" \
        --expected-edid-sha256 "$expected_edid" \
        --source-input "$source_input" \
        --target-input "$target_input" \
        --unsafe-allow-input-write \
        --acknowledge-switch-away \
        --recovery-method "$recovery_method" >"$artifact_dir/input-write.json"
}

case "$mode" in
    quick) run_quick ;;
    full) run_full ;;
    platform) run_platform ;;
    hardware-read) run_hardware_read ;;
    hardware-write) run_hardware_write ;;
    *) printf 'usage: %s quick|full|platform|hardware-read|hardware-write [options]\n' "$0" >&2; exit 2 ;;
esac

status=passed
