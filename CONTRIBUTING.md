# Contributing to BPX

Thanks for your interest in contributing to `bpx`.

## Before You Start

- For large changes, open an Issue first and align scope.
- Keep one concern per PR.
- Treat Unreal Engine source as behavioral reference only.
- Do not copy Unreal Engine source text into this repository.

## Development Setup

```bash
git clone https://github.com/wilddogjp/openbpx.git
cd openbpx
go mod download
```

## Required Local Checks

Run these before opening or updating a PR:

```bash
gofmt -l .
go vet ./...
staticcheck ./...
go test ./...
```

Notes:

- `gofmt -l .` must produce no output.
- If `staticcheck` is not installed, install it with:
  - `go install honnef.co/go/tools/cmd/staticcheck@latest`

## PR Requirements

Each PR description should include:

- What changed
- Why it changed
- How it was tested

When changing CLI behavior (flags/output/contract):

- Update [docs/commands.md](docs/commands.md)
- State compatibility impact clearly
- Use `BREAKING CHANGE` in the PR title when applicable

## Commit Conventions

Use Conventional Commit prefixes:

- `feat:`
- `fix:`
- `docs:`
- `test:`
- `refactor:`
- `ci:`
- `chore:`

## Testing and Fixtures

- Use existing fixtures under `testdata/`.
- For parser/rewrite safety changes, add focused tests near the modified package.
- If test coverage is not feasible, document the gap and manual validation performed.

### Local UE Path Configuration (Contributors)

For fixture-generation workflows, you can keep local Unreal/Lyra paths in a machine-local config file.
This avoids passing long path flags on every run.

```bash
# One-time local setup
cp scripts/local-fixtures.config.example.json scripts/local-fixtures.config.json
```

Edit `scripts/local-fixtures.config.json` and set at least:

- `engines`: engine-profile keyed settings (for example `5.6.1`, `5.7.3`)
- `engines.<version>.lyraRoot`: Lyra root per engine profile

Then run fixture scripts without path flags:

```bash
./scripts/gen-fixtures.sh --scope 1,2
```

Notes:

- `scripts/local-fixtures.config.json` is gitignored and must not be committed.
- Each `engines.<version>` entry should include at least `lyraRoot`; `ueEngineRoot` is strongly recommended.
- `gen-fixtures.sh` and `sync-bpx-plugin.sh` process all configured `engines` profiles in one run (in parallel when multiple profiles are present).
- You can use a non-default config path via `--config <path>`.
- CLI flags still override config values when both are specified.

## Security

Please do not report vulnerabilities in public issues.
See [SECURITY.md](SECURITY.md) for private reporting instructions.
