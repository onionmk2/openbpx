---
name: bpx-blueprint
description: BPX `blueprint` command skill. Inspect and analyze blueprint exports, bytecode, and graph data.
---

# blueprint

> **PREREQUISITE:** Read [bpx-shared](../bpx-shared/SKILL.md).

## Usage

```bash
bpx blueprint info <file.uasset> [--export <n>]
bpx blueprint bytecode <file.uasset> --export <n> [--range-source auto|export-map|ustruct-script|serial-full] [--strict-range] [--diagnostics]
bpx blueprint disasm <file.uasset> --export <n> [--format json|toml|text] [--analysis] [--entrypoint <vm>] [--max-steps <n>] [--range-source auto|export-map|ustruct-script|serial-full] [--strict-range] [--diagnostics]
bpx blueprint trace <file.uasset> --from <Node|Node.Pin> [--to-node <token>] [--to-function <token>] [--max-depth <n>]
bpx blueprint call-args <file.uasset> --member <token> [--class <token>] [--all-pins] [--include-exec]
bpx blueprint refs <file.uasset> --soft-path <path> [--class <token>] [--include-routes] [--max-routes <n>] [--max-depth <n>]
bpx blueprint search <file.uasset> [--class <token>] [--member <token>] [--name <token>] [--show <fields>] [--limit <n>]
bpx blueprint infer-pack <file.uasset> --export <n> [--entrypoint <vm>] [--max-steps <n>] [--out <dir>] [--range-source auto|export-map|ustruct-script|serial-full] [--strict-range] [--diagnostics]
bpx blueprint scan-functions <directory> --recursive [--name-like <regex>] [--aggregate]
```

## Behavior

- `info`: summarizes blueprint/function exports.
- `bytecode`: extracts selected bytecode range as base64.
- `disasm`: disassembles bytecode (json|toml|text, optional analysis).
- `trace`: traces an execution path between nodes.
- `call-args`: inspects call-node argument pins/defaults.
- `refs`: reverse-searches soft-path usage on node pins.
- `search`: token-searches nodes/pins in one blueprint package.
- `scan-functions`: aggregates function names across a directory.
- `infer-pack`: emits CFG/callsite/def-use inference artifacts.
- `bytecode`/`disasm` support range selection (`auto|export-map|ustruct-script|serial-full`).

## Command Matrix

| Command | Use when | Notable defaults |
|------|------|------|
| `info` | summarizes blueprint/function exports. | Read-only path; safe for discovery. |
| `bytecode` | extracts selected bytecode range as base64. | Check `bpx help` for exact required flags. |
| `disasm` | disassembles bytecode (json\|toml\|text, optional analysis). | Check `bpx help` for exact required flags. |
| `trace` | traces an execution path between nodes. | Check `bpx help` for exact required flags. |
| `call-args` | inspects call-node argument pins/defaults. | Check `bpx help` for exact required flags. |
| `refs` | reverse-searches soft-path usage on node pins. | Check `bpx help` for exact required flags. |
| `search` | token-searches nodes/pins in one blueprint package. | Check `bpx help` for exact required flags. |
| `infer-pack` | emits CFG/callsite/def-use inference artifacts. | Check `bpx help` for exact required flags. |
| `scan-functions` | aggregates function names across a directory. | Check `bpx help` for exact required flags. |

## Code-Aligned Caveats

- Large blueprints can produce very large payloads; constrain via `--limit`/`--max-steps`.
- `refs --include-routes` can be expensive; disable routes when doing broad scans.
- `disasm --entrypoint` implies analysis-oriented output.

## High-Signal Examples

```bash
bpx blueprint info ./Sample.uasset [--export 1]
bpx blueprint bytecode ./Sample.uasset --export 1 [--range-source auto|export-map|ustruct-script|serial-full] [--strict-range] [--diagnostics]
bpx blueprint disasm ./Sample.uasset --export 1 [--format json|toml|text] [--analysis] [--entrypoint 0] [--max-steps 1] [--range-source auto|export-map|ustruct-script|serial-full] [--strict-range] [--diagnostics]
bpx blueprint trace ./Sample.uasset --from <Node|Node.Pin> [--to-node SampleToken] [--to-function SampleToken] [--max-depth 1]
```
