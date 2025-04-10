# spotter

run all scripts from repo root

run all tests from repo root

go unit tests:

```bash
go test ./apps/spotter-manager/internal/handlers
```

python tests:

```bash
# unit
pytest -m "not integration" ./apps/spotter

# integration
pytest -m "integration" ./apps/spotter
```
