# Financy backend

Financy is a personal ledger: a quiet place to keep household accounts in good order. You record money as it moves — salary in, groceries out, a transfer from one bank to another — and the books stay in balance.

This repository is the backend. It stores the official books, talks to the [European Central Bank](https://www.ecb.europa.eu/) for exchange rates, and answers the screens you use day to day.

If you are keeping accounts for the first time, start with the [handbook](docs/README.md). It explains the ideas in everyday language: double-entry, a home currency, foreign bank accounts, and how net worth and income stay related.

## What it does

- **Double-entry books.** Every transaction has two sides. Money never appears from nowhere or disappears into nowhere.
- **A home currency you choose.** Reports are measured in one currency — the one you think in. Bank accounts keep their own currencies (euros stay euros, rupees stay rupees).
- **FIFO lots.** When you spend foreign cash, the app uses the oldest batch first and remembers what that batch originally cost in your home currency.
- **Official ECB rates.** Daily reference rates from the European Central Bank are built in. Weekends and holidays use the last published print.
- **Several currencies at once.** You can hold accounts in any of the currencies the ECB publishes.
- **Restatement when you ask.** If your life moves and you change home currency, remaining carrying amounts are restated at that day’s rate. History of the original bank amounts is left as it was.
- **Retranslation when you ask.** Income and net worth are compared in the home currency. The difference is the paper movement on foreign cash you still hold. You can look at it anytime, and record it when you are ready.

Behind the scenes, **CockroachDB** holds the ledger of record and **MongoDB** holds preferences plus a cache so everyday screens can load without rereading the whole ledger. See [How data is stored](docs/how-data-is-stored.md).

## Run it locally

You need [Podman](https://podman.io/) (or Docker with `COMPOSE=docker compose`) and Go 1.27.

```bash
make up
```

That starts CockroachDB, MongoDB, and the API with live reload.

| What | Where |
|---|---|
| API | http://localhost:8081 |
| Health | http://localhost:8081/api/health |
| CockroachDB SQL | `postgresql://root@localhost:26257/defaultdb?sslmode=disable` |
| CockroachDB UI | http://localhost:8080 |
| MongoDB | `mongodb://127.0.0.1:27017/financy?directConnection=true` |

Local sign-in uses the development login (`DEV_LOGIN=1`) for the allowlisted address in [`.env.example`](.env.example).

```bash
make tests    # go test against local CRDB and Mongo
make logs     # follow compose logs
make down     # stop containers
```

Copy `.env.example` if you run the server outside compose. `SESSION_SECRET` and `OAUTH_CLIENT_ID` are required in every environment; `ALLOWED_USERS` is the comma-separated Google account list.

## Layout

```
cmd/server          HTTP process
cmd/migrate         schema migrations
internal/domain     money, lots, dates, valuation
internal/ledger     posting, FIFO, views, restatement
internal/store      CockroachDB
internal/mongo      settings, flags, cache, ECB prints
internal/httpapi    JSON API
internal/auth       session cookie and allowlist
docs                handbook
```

Version is the single line in [`VERSION`](VERSION). The health endpoint reports the same string.

## Handbook

| Page | About |
|---|---|
| [Handbook home](docs/README.md) | Map of the ideas |
| [Getting started](docs/getting-started.md) | First setup |
| [Double-entry](docs/double-entry.md) | Why every transaction has two sides |
| [Home currency](docs/home-currency.md) | The currency the books are measured in |
| [Foreign cash and lots](docs/foreign-cash-and-lots.md) | Multi-currency accounts and FIFO |
| [Exchange rates](docs/exchange-rates.md) | ECB rates baked in |
| [Changing home currency](docs/changing-home-currency.md) | Restatement on demand |
| [Net worth and retranslation](docs/net-worth-and-retranslation.md) | How income and net worth stay related |
| [How data is stored](docs/how-data-is-stored.md) | CockroachDB, MongoDB, and the cache |
| [Dates and calendar](docs/dates-and-calendar.md) | UTC journals and your timezone |
| [Glossary](docs/glossary.md) | Short definitions |
