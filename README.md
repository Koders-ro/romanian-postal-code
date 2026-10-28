# Romanian Postal Code API

A fast, small Go API for working with Romanian postal codes (coduri poștale). It can:

- **Validate** a code: check the 6-digit format and whether it exists.
- **Decode** a code: county, locality, sector, and the street plus number range it covers.
- **Verify** an address combo (code, county, city, street, number) and say which part is wrong.
- **Look up** the code for an address, down to the house-number range or block.
- **Normalize** messy input:
  - diacritics: `ș/ş/ț/ţ/ă/â/î` all fold to ASCII
  - spacing and punctuation
  - street-type prefixes: `str.`, `strada`, `bd.`, `b-dul`, `șos.`, `cal.`…
  - honorific titles: `gen.`, `dr.`, `arh.`…
  - word order: the official data stores `Mincu Ion, arh.` for "Strada Arh. Ion Mincu"

`calea mosilor 120`, `mosilor 120`, `cal. mosilor 120`, `cal mosilor 120` and
`CALEA MOȘILOR, NR. 120, BL. 3, AP. 4` all resolve to the same street and number.

**Performance:**
- The server uses only the Go standard library.
- The dataset is embedded in a ~7.5 MB static binary.
- Startup takes about 100 ms.
- A verification takes under 1 µs, served from in-memory maps.

## Postal code format

Every Romanian postal code has 6 digits:

| Digit(s) | Meaning |
|---|---|
| 1 | Postal region. `0` is Bucharest, Ilfov and Giurgiu. |
| 2 | County within the region. For Bucharest it is the sector (`01xxxx`…`06xxxx`). |
| 3–6 | Locality, street, street segment or building. |

Cities over about 50,000 people have codes per street or street segment. Every other locality has one code.

Number ranges keep the parity of their first number:
- `nr. 1-25` covers the odd numbers 1 to 25.
- `nr. 2-T` covers the even numbers from 2 to the end (*T = până la capăt*).

## Run

```bash
docker build -t ro-postal .
docker run -e API_KEY=secret -p 3000:3000 ro-postal
curl -H "X-API-Key: secret" localhost:3000/v1/codes/400693
```

`docker-compose.yml` is made for Coolify. It only exposes port 3000 to the proxy network.
To use it on a machine without a proxy, add `ports: ["3000:3000"]`.

Without Docker (Go 1.23+):

```bash
API_KEY=secret go run ./cmd/server
```

### Deploy on Coolify

1. **New Resource → Public/Private Git repository**, then pick this repo.
2. Choose a build pack:
   - **Dockerfile:** keep **Ports Exposes = 3000**, which is Coolify's default and the app's default.
     Leave **Ports Mappings** empty, and don't set `PORT`.
   - **Docker Compose:** set the service's domain as `https://your.domain:3000`.
     The `:3000` tells Coolify's proxy which container port to use; the public URL stays on 443.
3. Add the env vars `API_KEY` and `TRUST_PROXY=true`.
4. Deploy. The image has a built-in `HEALTHCHECK`, and `/healthz` needs no auth.

If the deployment shows *"no available server"* or a 502, the port Coolify routes to doesn't match the
port the app listens on (`PORT`, default 3000). If it fails with *"port is already allocated"*, remove
any **Ports Mappings**. They publish the port on the host, which the proxy doesn't need.

| Env | Default | |
|---|---|---|
| `API_KEY` | — | **Required.** The server refuses to start without it. Use a long random value (`openssl rand -hex 32`). Comma-separate several keys to rotate them without downtime. |
| `PORT` | `3000` | Port the server listens on inside the container. |
| `MAX_BATCH` | `500` | Maximum items per batch request. |
| `RATE_LIMIT` | `0` (off) | Requests per minute per client IP on `/v1/*`, with a burst of about 10 seconds' worth. Over the limit, the server returns `429` with `Retry-After`. |
| `TRUST_PROXY` | `false` | Set to `true` behind Coolify/Traefik, nginx or Cloudflare, so logs and rate limits use the real client IP from `X-Forwarded-For`. Leave it `false` if the port is exposed directly. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |

Invalid values stop the server at startup with a clear error.

To set the version shown in `/` and `/healthz`, build with `docker build --build-arg VERSION=1.0.0 .`.

### Production behaviour

- **Logging:** one JSON log line per request on stdout, with request ID, method, path, status, duration and client IP. Query strings are never logged, so `?api_key=` can't leak into logs. Successful health checks are not logged.
- **Request IDs:** an incoming `X-Request-ID` is passed through. Otherwise one is generated, and it is returned in the response.
- **Auth:** API keys are compared in constant time.
- **Headers:** responses send `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` and `Cache-Control: no-store`.
- **Request limits:**
  - JSON bodies: 1 MB
  - headers: 16 KB
  - each input field: 200 characters
  - timeouts on header read, body read, write and idle connections
- **Resilience:** panics are recovered and returned as `500`. On `SIGTERM` the server shuts down gracefully with a 10-second drain.
- **Container:**
  - a `scratch` image running as a non-root user
  - `docker-compose.yml` adds a read-only filesystem, drops all capabilities and sets `no-new-privileges`
  - a 256 MB memory limit; the process uses about 80 MB
  - rotated log files

On Coolify, put the service behind its proxy with a domain and HTTPS, and set `TRUST_PROXY=true`.
The service is stateless, so you can run several replicas behind a load balancer. The rate limit is counted per instance.

## API

You can authenticate in any of three ways:
- the `X-API-Key: <key>` header
- the `Authorization: Bearer <key>` header
- the `?api_key=<key>` query parameter

All responses are JSON and are gzipped when the client accepts it.

### `GET /v1/codes/{code}`: validate and decode a code

```bash
curl -H "X-API-Key: $K" localhost:3000/v1/codes/020881
```
```json
{
  "input": "020881", "code": "020881", "format_valid": true, "exists": true,
  "region": 0, "county": "București", "locality": "București", "sector": 2,
  "level": "street_segment",
  "entries": [{
    "code": "020881", "county": "București", "locality": "București", "sector": 2,
    "street_type": "Cale", "street": "Moșilor", "numbers": "nr. 96-126", "siruta": "179150",
    "label": "Calea Moșilor nr. 96-126, București, Sector 2, 020881",
    "label_ascii": "Calea Mosilor nr. 96-126, Bucuresti, Sector 2, 020881"
  }]
}
```

Status codes:
- `400`: bad format.
- `404`: the code has a valid format but doesn't exist. The response then includes `probable_counties`.

`level` is one of:
- `locality`: the whole village or town has this code.
- `street`: the whole street has this code.
- `street_segment`: the code covers a number range or a set of blocks.

### `GET|POST /v1/verify`: check an address

All fields are optional. Send whatever you have: `code`, `county`, `city`, `street`, `number`.
The number can also be part of `street`.

```bash
curl -G -H "X-API-Key: $K" localhost:3000/v1/verify \
  --data-urlencode "code=020876" --data-urlencode "city=Bucuresti" \
  --data-urlencode "street=cal. mosilor 120"
```
```json
{
  "valid": false,
  "normalized": {"code": "020876", "city": "bucuresti",
                 "street": {"type": "calea", "name": "mosilor", "number": "120"}},
  "checks": {"code_format": true, "code_exists": true, "city": true, "street": true,
             "number": true, "code_matches_address": false},
  "expected_codes": ["020881"],
  "matches": [{"code": "020881", "street": "Moșilor", "numbers": "nr. 96-126", "...": "..."}],
  "messages": ["postal code 020876 does not match the address"]
}
```

- `valid` is true only when every check that ran passed.
- A check that couldn't run is omitted. For example, `street` is skipped for villages that have a single code.
- On a miss, `suggestions` lists the closest counties, cities or streets:
  `"Cal. Mosilr"` → `"Calea Moșilor, București"`.

Examples of what you can send:
- `code` + `city`
- `code` + `county`
- `code` + `street`, with the city inferred from the code
- `county` + `city` + `street` + `number`
- `city=Sector 3` or `city=București S3`

Counties are accepted by name or 2-letter code, e.g. `CJ`, `jud. Cluj`, `B`.

`POST` takes the same fields as JSON:
```bash
curl -H "X-API-Key: $K" -d '{"county":"CJ","city":"Cluj-Napoca","street":"Str. General Eremia Grigorescu","number":"10"}' \
  localhost:3000/v1/verify
```

### `POST /v1/verify/batch`
```json
{"items": [{"code": "400693", "city": "Cluj-Napoca"}, {"code": "999999"}]}
```
This returns `{"results": [...]}`, one verify result per item.

### `GET /v1/lookup?county=&city=&street=&number=`: address → code
```bash
curl -G -H "X-API-Key: $K" localhost:3000/v1/lookup --data-urlencode "city=Sector 2" --data-urlencode "street=mosilor 255"
# {"found":true,"codes":["020878"],"matches":[...], ...}
```

### `GET /v1/normalize?street=&city=&county=&code=`: debugging helper
```bash
curl -G -H "X-API-Key: $K" localhost:3000/v1/normalize --data-urlencode "street=B-dul Ion C. Brătianu nr. 5A bl. 3 ap. 12"
# {"street":{"type":"bulevardul","name":"ion c bratianu","key":"bratianucion","number":"5a","block":"3", ...}}
```

### `GET /healthz`
This endpoint needs no auth. It returns the dataset statistics.

## Data

The data is Poșta Română's official **Infocod** file ("Infocod Sept. 2016 cu SIRUTA"), published on
[data.gov.ro](https://data.gov.ro/dataset/coduri-postale-romania) under the open
OGL-ROU-1.0 license. It holds 55,407 rows, 37,914 codes, 13,851 localities and 23,800 streets.
[coduripostale.ro](https://www.coduripostale.ro/) shows the same data as a website.

`data/postal.csv.gz` is generated from the XLS and embedded at build time. To refresh it from a newer Infocod release:

```bash
cd tools/importer
go run . -in /path/to/infocod.xls -out ../../data/postal.csv.gz
```

The importer is a separate Go module, so the server keeps zero dependencies. Any CSV with the columns
`county,locality,siruta,street_type,street,number,code,sector` (gzipped) also works.

## Project layout

```
internal/normalize   diacritics, street/locality/county parsing
internal/store       in-memory index, number-range matching, verify/decode, suggestions
internal/api         HTTP handlers + middleware (auth, rate limit, logging, gzip, security headers)
cmd/server           entrypoint, env config (+ -healthcheck flag for the scratch image)
tools/importer       XLS → data/postal.csv.gz
```

## License

[MIT](LICENSE) © 2026 Capota Cristian ([Koders.ro](https://koders.ro)).

The embedded postal dataset comes from Poșta Română via data.gov.ro under OGL-ROU-1.0.
