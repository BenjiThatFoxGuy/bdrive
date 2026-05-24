# Tests

The test tree is organized by behavior boundary.

## `tests/api`

HTTP API contract tests. These tests start the generated API server with real
repositories and fake Telegram only where the route requires Telegram behavior.

Use these tests for:

- route status codes
- response bodies and stable error codes
- auth behavior
- request ID behavior
- important API-visible side effects

Do not use raw SQL here. Seed or verify through repositories unless the test is
only setting up the harness.

## `tests/db`

Repository and database behavior tests against PostgreSQL.

Use these tests for:

- repository method behavior
- `repositories.ErrNotFound` / conflict behavior
- folder tree and folder size behavior
- upload/share/session persistence rules

Do not test generated API code here, and do not use raw SQL for behavior tests.

## Running

```bash
docker compose -f tests/docker-compose.test.yml up -d
TEST_DATABASE_URL=postgres://teldrive_test:teldrive_test@localhost:55432/teldrive_test?sslmode=disable go test ./tests/api ./tests/db -count=1
```

Or:

```bash
task test:int
```
