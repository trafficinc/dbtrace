# dbtrace

See exactly what changed in MySQL after a web-app action.

`dbtrace` is a developer CLI for tracing database behavior. Take a snapshot, perform an action in your app, take another snapshot, and get a grouped report of inserted, updated, and deleted values.

## Why dbtrace?

When working with an unfamiliar or legacy application, a single action can quietly affect several tables.

Instead of manually inspecting the database or tracing application code:

```text
dbtrace before
# perform the action
dbtrace after
```

dbtrace tells you what changed.

## Good use cases

- Reverse-engineering legacy applications
- Debugging unexpected side effects
- Understanding unfamiliar codebases
- Checking what a form submission actually changes
- Testing integrations
- Validating business workflows
- Discovering audit/history tables
- Investigating database writes during development

> dbtrace does not modify application data. It reads database state to build and compare snapshots.

## Build

Build release binaries:

```bash
./scripts/build.sh
```

The build script uses `CGO_ENABLED=0` and produces:

```text
dist/dbtrace-darwin-amd64
dist/dbtrace-darwin-arm64
dist/dbtrace-windows-amd64.exe
```

If your system Go is older than required, `scripts/go.sh` downloads a workspace-local Go toolchain into `.tools/`.

Manual build commands:

```bash
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o dist/dbtrace-darwin-amd64 ./cmd/dbtrace
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o dist/dbtrace-darwin-arm64 ./cmd/dbtrace
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/dbtrace-windows-amd64.exe ./cmd/dbtrace
```

Run tests:

```bash
./scripts/test.sh
```

Run MySQL integration tests only when you have a disposable database available:

```bash
MYSQL_DSN='user:pass@tcp(localhost:3306)/app_db' ./scripts/test.sh
```

## Quickstart

If you have an app that uses MySQL:

1. Build the tool:

```bash
./scripts/build.sh
```

2. In your app's project folder, create `dbtrace.yaml`:

```bash
/path/to/dbtrace/dist/dbtrace-darwin-arm64 init --dsn="user:pass@tcp(localhost:3306)/your_app_db"
```

Use `dbtrace-darwin-amd64` on Intel Mac, or `dbtrace-windows-amd64.exe` on Windows.

3. Take a before snapshot:

```bash
/path/to/dbtrace/dist/dbtrace-darwin-arm64 before
```

4. Use your app normally:

```text
Click button, submit form, run workflow, etc.
```

5. Take the after snapshot:

```bash
/path/to/dbtrace/dist/dbtrace-darwin-arm64 after
```

It will automatically print what changed in MySQL:

```text
► Table: users
  * Updates (1)
       id=1 last_login: 2025-06-02 07:26:58 → 2025-06-15 09:22:35
```

For repeated testing:

```bash
/path/to/dbtrace/dist/dbtrace-darwin-arm64 watch
```

Then press Enter after each app action.

By default, snapshot progress is quiet:

```text
Scanning...
Complete.
```

Use `--verbose` when you want per-table scan details:

```bash
/path/to/dbtrace/dist/dbtrace-darwin-arm64 before --verbose
/path/to/dbtrace/dist/dbtrace-darwin-arm64 after --verbose
/path/to/dbtrace/dist/dbtrace-darwin-arm64 watch --verbose
```

Verbose mode prints lines like:

```text
Scanning: users
Scanned: users (120 rows)
```

## Configuration

Create a config automatically:

```bash
dbtrace init --dsn="user:pass@tcp(localhost:3306)/app_db"
```

Or copy `dbtrace.yaml.example`:

```yaml
database:
  dsn: "user:pass@tcp(localhost:3306)/app_db"

snapshot:
  workers: 4
  chunk_size: 10000
  output_dir: ".dbtrace/snapshots"

report:
  max_lines_per_operation: 50
  max_value_length: 200

keys:
  # Optional for legacy tables without primary keys.
  # legacy_data_table:
  #   - project_id
  #   - record
  #   - field_name
  #   - event_id
  #   - instance

ignore:
  tables:
    - sessions
    - cache
    - jobs
  columns:
    - created_at
    - updated_at
    - last_seen
```

`dbtrace init` can also detect Laravel-style `.env` values such as `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, and `DB_PASSWORD`.

On Windows, quote DSNs because shells treat characters like `@`, `&`, `%`, and parentheses differently:

```powershell
.\dbtrace-windows-amd64.exe init --dsn "user:pass@tcp(localhost:3306)/app_db"
```

For passwords with special characters, prefer `.env` variables or URL-escaped password values.

## Legacy Tables And Composite Keys

`dbtrace` chooses row identity in this order:

1. configured `keys:` entries
2. database primary keys, including composite primary keys
3. the smallest unique index
4. synthetic row identity from the row hash

For older applications or legacy-style schemas, configure logical keys explicitly:

```yaml
keys:
  legacy_data:
    - project_id
    - record
    - field_name
    - event_id
    - instance
```

Composite key output looks like:

```text
project_id=1|record=1001|field_name=dob value: 1980-01-01 → 1980-02-01
```

Synthetic identity is a last resort. It can detect inserts/deletes, but an update may appear as one deleted row and one inserted row because there is no stable database identity.


## Workflow

```bash
dbtrace before
# perform action in the app
dbtrace after
```

`dbtrace after` captures the second snapshot and runs the diff automatically.

You can diff existing snapshots without taking a new snapshot:

```bash
dbtrace diff
```

For repeated manual testing:

```bash
dbtrace watch
```

Watch mode takes an initial `before` snapshot, waits for Enter, captures `after`, prints the diff, rotates `after` into `before`, and waits again.

## Example Output

```text
RESULT:
3 tables changed

► Table: users
  * Updates (1)
       id=1 last_login: 2025-06-02 07:26:58 → 2025-06-15 09:22:35

► Table: orders
  * Updates (2)
       id=10 status: pending → paid
       id=10 paid_at: NULL → 2026-06-15 09:22:35

► Table: payments
  * Inserts (1)
       id=991 id: NULL → 991
       id=991 amount: NULL → 49.00
```

## Ignore Rules

Use ignored tables for noisy or irrelevant data:

```yaml
ignore:
  tables:
    - sessions
    - cache
```

Use ignored columns for values that change often but do not explain the action:

```yaml
ignore:
  columns:
    - created_at
    - updated_at
    - last_seen
```

Ignored columns are excluded from row hashes and stored row values.

## Performance Notes

`dbtrace` is designed for large tables:

- streams rows instead of loading tables into memory
- uses keyset pagination, not `OFFSET`
- scans tables with parallel workers
- stores snapshots in SQLite
- diffs snapshots with indexed SQLite joins
- compares row hashes first, then reads full values only for changed rows

## Limitations

- synthetic identity is less precise than configured keys or database keys
- duplicate rows in no-key tables may collapse under synthetic identity
- very large column values may be truncated in terminal output
- full column values remain in SQLite snapshots for future export/reporting

## Platform Support

Release builds target:

- macOS amd64
- macOS arm64
- Windows amd64

The project uses a pure-Go SQLite driver and `filepath`-safe paths so builds and snapshot paths work on macOS and Windows.
