# Spec: topic-scoped heal — already built as `ward wave`

## Honest framing: already implemented

The "adaptation loop" this roadmap labeled skill-sharpening is already fully
built as **`ward wave <topic> [--heal]`** (`internal/cli/fleet.go:19-98`):

- Live re-verifies every accepted artifact carrying the `<topic>` tag,
  persisting results (`SetVerify`).
- Counts verified vs drifted, and with `--heal` supersedes any drifted artifact
  (reason `"wave drift"`).
- `--json` output; covered by `TestWaveVerifiesCatchesDriftAndHeals`
  (`internal/cli/surface_test.go:181`).

The earlier versions of this spec proposed a new `SharpenAll` loop and a
`ward skill sharpen` command. Both are duplicates — `tick`/`brief` give the
whole-store sweep, `ward wave <topic>` gives the topic-scoped sweep. **No
build is needed. This spec is closed.**

## Verification gate (proof it exists, no code expected)

```bash
# Regression proof
go test ./internal/cli/... -run TestWaveVerifiesCatchesDriftAndHeals -v

# CLI smoke (a topic tag with a local artifact)
ward wave <some-topic-tag> --json        # re-verify + count
ward wave <some-topic-tag> --heal --json # plus supersede drift
```

If a gap shows up while running the gate (e.g. wave's behavior diverges from
`tick --heal`), fix that; otherwise close without production changes.