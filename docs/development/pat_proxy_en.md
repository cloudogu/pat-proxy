# PAT Proxy: Functionality, Configuration, and Upstream Adapters

## What is the proxy for?

A Personal Access Token (PAT) is a personal access credential. A client can use
it to call an API without performing an interactive login for every request.
The PAT proxy validates these credentials with the Central Authentication
Service (CAS) and forwards authorized requests to a configured HTTP service.
In the Cloudogu environment, this service is a Dogu or an adapter in front of it.

The proxy handles PAT validation. An upstream adapter translates the verified
user identity into the authentication method understood by the respective Dogu.
For example, it can use a local API key or obtain a service-specific token.

```text
Client ──HTTP──> PAT Proxy ──HTTP──> Upstream Adapter ──HTTP──> Dogu/API
                    │
                    └──GET with Basic Auth──> CAS: validate PAT
```

“Upstream” refers to the next service to which the proxy forwards a request.
An adapter is optional if the Dogu already supports the proxy's internal
authentication contract itself. The proxy does not create Dogu tokens or
implement service-specific authentication.

## How is a request processed?

### 1. Remove internal headers

The proxy removes all headers whose names begin with `X-Internal-Auth-` from
every incoming request. This comparison is case-insensitive. This prevents a
client from simply supplying a username in this header to fake successful
PAT validation.

### 2. Identify PAT requests

A request is validated with CAS only if all of the following conditions are met:

- Validation is enabled; this is the default setting.
- The path is within the configured API path range.
- The `Authorization` header contains correctly encoded HTTP Basic Auth with a
  username and a password.
- The password begins with exactly `pat_`.

A client therefore sends the PAT as the Basic Auth password, for example:

```bash
curl --user 'alice:pat_EXAMPLE' http://127.0.0.1:8080/api/projects
```

With `--apiPath /api`, `/api` and `/api/projects` match, but `/apix` does not.
`--apiPath /` covers all paths. For an API path range, a value without a trailing
slash is recommended. Detection uses the URL-encoded path; it does not normalize
or decode it beforehand.

Other requests are forwarded without CAS validation. This includes ordinary
Basic Auth passwords, Bearer tokens, requests without credentials, and paths
outside the API path range. Malformed Basic Auth encoding does not trigger PAT
validation either. The application or adapter must handle these requests itself.
The proxy therefore does not require every request to contain a PAT.

### 3. Validate the PAT with CAS

For each identified PAT request, the proxy sends a separate `GET` request to the
fully configured CAS validation URL. It uses the original `Authorization` header.
The body of the client request is not sent to CAS. The result is not cached:
even the same PAT is validated again on subsequent requests.

A configured scope is appended as a URL-encoded `scope` query parameter.
For example, `--scope /mydogu` produces the parameter `scope=%2Fmydogu`. Existing
`scope` parameters are replaced; the contents of other query parameters are
preserved. Without the scope option, the configured URL remains unchanged,
including any scope already specified in it.

| CAS request result | PAT proxy behavior |
| --- | --- |
| HTTP `200` | Forward the request to the upstream. |
| HTTP `400` | The client receives `400 Bad Request`; no forwarding. |
| HTTP `401` | The client receives `401 Unauthorized`; no forwarding. |
| Any other status, including `403`, `404`, `500`, or a redirect | The client receives `502 Bad Gateway`; no forwarding. |
| Connection error, TLS error, or timeout | The client receives `502 Bad Gateway`; no forwarding. |

Redirects are not followed. The CAS response body is neither evaluated nor passed
on to the client. The identity comes from the request's Basic Auth username after
CAS has accepted these credentials with HTTP `200`.

### 4. Pass the verified identity to the upstream

After successful validation, the proxy removes the `Authorization` header and sets:

```http
X-Internal-Auth-Method: cas-pat
X-Internal-Auth-User: YWxpY2U=
```

`YWxpY2U=` is the UTF-8 username `alice` encoded using standard Base64.
The adapter therefore receives the verified identity, but does not receive the
original PAT through the Authorization header.

For requests without successful PAT validation, the proxy does not set these two
headers. The original `Authorization` header is then preserved. Even when
validation is disabled, incoming internal authentication headers are removed,
while ordinary credentials are forwarded.

### 5. Forward the request and return the response

The proxy sets the connection destination to the configured upstream. The method,
body, path including encoded path components, query, and original HTTP Host are
preserved. The HTTP Host can therefore differ from the hostname of the connection
destination.

The values of `X-Forwarded-For`, `X-Forwarded-Host`, and `X-Forwarded-Proto` are
preserved if present. The proxy does not generate these values itself. A trusted
proxy in front of it should sanitize or set them before they are used for security
decisions.

Standard HTTP reverse proxy processing removes hop-by-hop headers, such as
`Connection`; a byte-for-byte copy of all headers is therefore not guaranteed.
Upstream responses are passed on to the client, including error responses.
If forwarding fails, the proxy returns `502`.

## Building and starting

All build commands below are run from the root directory of this repository.
According to `go.mod`, the project requires Go **1.26.0**.

### Build with Make and Docker

The local Make build requires Make and Docker with an accessible Docker daemon.
The build uses the `golang:1.26.0` image.

```bash
make
```

The resulting executable is located at `target/pat-proxy`. Make also creates a
tar archive in the `target` directory. The CLI application name `patproxy` is
not the name of the executable generated by Make.

### Build directly with Go

With a suitable local Go installation, you can also build without Docker.
The dependencies are included in the `vendor` directory:

```bash
mkdir -p target
go build -mod=vendor -o target/pat-proxy .
```

### Start with PAT validation

The upstream and CAS must first be reachable. The names and scope in the
following example must be adapted to your environment:

```bash
./target/pat-proxy \
  --listenAddress 127.0.0.1:8080 \
  --upstreamHost 127.0.0.1 \
  --upstreamPort 8081 \
  --apiPath /api \
  --casUrl 'https://cas.example.org/cas/api/pats/validate' \
  --scope /mydogu \
  --validationTimeout 5
```

The client connects to port `8080`; the proxy forwards to port `8081`.
`/cas/api/pats/validate` is an example validation path: enter the actual complete
endpoint of your CAS instance, not just its base address.

### Start locally without PAT validation

For a local test with an upstream that is already running:

```bash
./target/pat-proxy -la 127.0.0.1:8080 -uh 127.0.0.1 -up 8081 \
  --validationEnabled=false
```

This mode does not validate credentials or create an internal user identity.
The upstream must process the forwarded credentials itself. Without a reachable
upstream, a request returns HTTP `502`.

### Use a Unix socket

Instead of a TCP connection, the proxy can use a Unix socket provided by the adapter:

```bash
./target/pat-proxy \
  -la 127.0.0.1:8080 \
  -us /run/mydogu/adapter.sock \
  -api /api \
  -cas 'https://cas.example.org/cas/api/pats/validate' \
  -s /mydogu
```

When a socket path is set, the connection uses this socket; host and port no longer
determine the network transport. The adapter must speak HTTP over the socket.
The socket and its parent directory must be accessible to the proxy process.
The port configuration is still checked for a valid value at startup.

## Configuration in detail

Configuration uses CLI options or the environment variables listed below.
No configuration file is currently read. CLI values take precedence over
environment variables; otherwise, the default values apply.

| Long option | Short option | Environment variable | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--listenAddress` | `-la` | `PAT_PROXY_LISTEN_ADDRESS` | `0.0.0.0:8080` | Address and port on which the proxy accepts HTTP requests. |
| `--upstreamHost` | `-uh` | `PAT_PROXY_UPSTREAM_HOST` | `127.0.0.1` | Hostname or IP address of the upstream. |
| `--upstreamPort` | `-up` | `PAT_PROXY_UPSTREAM_PORT` | `8081` | TCP port of the upstream. |
| `--upstreamSocket` | `-us` | `PAT_PROXY_UPSTREAM_SOCKET` | empty | Use a Unix socket instead of TCP. |
| `--apiPath` | `-api` | `PAT_PROXY_API_PATH` | `/` | Path range in which PAT requests are detected. |
| `--casUrl` | `-cas` | `PAT_PROXY_CAS_URL` | empty | Complete HTTP(S) URL for PAT validation. |
| `--scope` | `-s` | `PAT_PROXY_SCOPE` | empty | Scope for the CAS request. |
| `--validationTimeout` | `-t` | none | `5` | Timeout for the CAS request in seconds. |
| `--validationEnabled` | none | `PAT_PROXY_VALIDATION_ENABLED` | `true` | Enable or disable PAT validation. |
| `--casInsecure` | none | `PAT_PROXY_CAS_INSECURE` | `false` | Disable TLS certificate verification for CAS only. |

Example using environment variables:

```bash
export PAT_PROXY_LISTEN_ADDRESS='127.0.0.1:8080'
export PAT_PROXY_UPSTREAM_HOST='127.0.0.1'
export PAT_PROXY_UPSTREAM_PORT='8081'
export PAT_PROXY_API_PATH='/api'
export PAT_PROXY_CAS_URL='https://cas.example.org/cas/api/pats/validate'
export PAT_PROXY_SCOPE='/mydogu'
export PAT_PROXY_VALIDATION_ENABLED='true'
./target/pat-proxy --validationTimeout 10
```

There is no environment variable for `validationTimeout`. An invented variable
such as `PAT_PROXY_VALIDATION_TIMEOUT` would therefore have no effect.

The following conditions are checked at startup:

- `apiPath` is not empty and begins with `/`.
- `listenAddress` contains a host and port; for example, `127.0.0.1:8080` or `:8080`.
- The listen port and upstream port are between `1` and `65535`.
- `validationTimeout` is greater than `0`, even when validation is disabled.
- When validation is enabled, `casUrl` is an HTTP(S) URL with a hostname, without
  embedded credentials and without a fragment such as `#section`.

An invalid configuration prevents startup; the program exits with code `1`.
All options can be displayed with `./target/pat-proxy --help`.

The listener and upstream connection use HTTP. If TLS is required for incoming
connections, a service in front of the proxy must handle it. For CAS, the proxy
supports HTTPS with a minimum of TLS 1.2.
`--casInsecure` is intended for an environment where certificates are deliberately
left unverified; certificate verification should remain enabled during normal
operation.

The upstream transport ignores HTTP proxy environment variables. The separate
CAS transport, however, retains Go's default behavior and may take these
variables into account.

## Implementing an upstream adapter

### Architecture and responsibilities

The repository has no adapter registry, adapter interface, or plugin loading
function. An adapter is a standalone HTTP service referenced by `upstreamHost`
and `upstreamPort`, or by `upstreamSocket`. It can be implemented in Go or another
language with HTTP support.

A typical adapter performs these steps:

1. Accept a request from the PAT proxy.
2. Check that `X-Internal-Auth-Method` has the value `cas-pat`.
3. Decode `X-Internal-Auth-User` using standard Base64 and validate the username
   according to the target system's rules.
4. Map the verified CAS identity to a Dogu user.
5. Obtain or generate the credentials required for the Dogu.
6. Modify the request and forward it to the Dogu.
7. Return the response to the PAT proxy.

The internal authentication header is trustworthy only over a protected transport.
Base64 is an encoding, not encryption or a signature. The adapter should therefore
be accessible only through an internal connection, such as a Unix socket with
appropriate file permissions or an isolated network connection. Loopback alone
does not protect against other local processes. If a client can reach the adapter
directly, it could set both headers itself and bypass CAS validation.

Requests without an internal identity need an explicitly defined handling policy.
For example, an adapter can pass them through unchanged to the Dogu's normal
authentication mechanism or reject them with `401`. This decision depends on
whether the adapter should serve only PAT access or other access as well.

CAS validation does not replace authorization checks in the Dogu. The adapter
must not automatically grant unlimited permissions to a verified user.

### Which parts of the request can an adapter modify?

As a standalone service, the adapter can modify all parts of the request.
The PAT proxy provides no special configuration options for this; the changes
are implemented in the adapter.

| Part of the request | Possible change | What to consider |
| --- | --- | --- |
| Authentication | Supply a Dogu-specific Bearer token, Basic Auth, or session cookie. | The credentials must match the verified identity and its permissions. |
| Headers | Set an API key, add required headers, or remove other authentication headers. | Remove internal identity headers before calling the Dogu unless the Dogu itself needs them. |
| Destination and HTTP Host | Choose an internal Dogu endpoint and set the `Host` header accordingly. | Select destinations in a controlled manner; client input must not determine arbitrary connection destinations. |
| Path | Map `/api/projects` to `/internal/api/projects`, for example. | Handle encoded characters, subpaths, and slashes deliberately. |
| Query parameters | Add, replace, or remove parameters. | Handle multiple values and URL encoding correctly. |
| HTTP method | Translate a method for the target API. | Consider the meaning, side effects, and repeatability of the request. |
| Body | Transform JSON or insert additional fields. | Limit the size; adjust `Content-Type` and `Content-Length`. Avoid unintentionally losing streaming by reading the entire body into memory. |

The adapter can also modify responses, such as response headers, cookies, or
redirect URLs. If it rewrites a response body, it must also account for its length
and, where applicable, compression.

### Example: A small Go adapter

The following example demonstrates the HTTP contract and request processing
before forwarding. The adapter accepts only validated PAT requests, decodes the
username, sets Basic Auth for the target system, and removes the internal headers.
It listens on `127.0.0.1:8081` and forwards to an example Dogu service on
`127.0.0.1:8082`.

The `doguPasswordFor` function is deliberately just a placeholder: it returns a
fixed test password only for the example user `alice`. A production adapter must
implement the actual user mapping and credential retrieval here. The password
should then be kept in a protected secret store, for example, rather than in the
source code.

```go
package main

import (
    "encoding/base64"
    "log"
    "net/http"
    "net/http/httputil"
    "net/url"
    "strings"
    "time"
)

func doguPasswordFor(username string) (string, bool) {
    if username != "alice" {
        return "", false
    }
    return "ONLY-A-TEST-PASSWORD", true
}

func removeInternalHeaders(headers http.Header) {
    for name := range headers {
        if strings.HasPrefix(strings.ToLower(name), "x-internal-auth-") {
            delete(headers, name)
        }
    }
}

func main() {
    target, err := url.Parse("http://127.0.0.1:8082")
    if err != nil {
        log.Fatal(err)
    }

    transport := http.DefaultTransport.(*http.Transport).Clone()
    transport.Proxy = nil

    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("X-Internal-Auth-Method") != "cas-pat" {
            http.Error(w, "Verified PAT identity required", http.StatusUnauthorized)
            return
        }

        decoded, err := base64.StdEncoding.DecodeString(
            r.Header.Get("X-Internal-Auth-User"),
        )
        if err != nil || len(decoded) == 0 {
            http.Error(w, "Invalid internal identity", http.StatusUnauthorized)
            return
        }
        username := string(decoded)
        password, ok := doguPasswordFor(username)
        if !ok {
            http.Error(w, "No Dogu access for this user", http.StatusForbidden)
            return
        }

        proxy := &httputil.ReverseProxy{
            Transport: transport,
            Rewrite: func(pr *httputil.ProxyRequest) {
                pr.SetURL(target)
                pr.Out.Host = target.Host
                removeInternalHeaders(pr.Out.Header)

                // Derive authentication for the Dogu from the verified identity.
                pr.Out.Header.Del("Authorization")
                pr.Out.Header.Del("Cookie")
                pr.Out.SetBasicAuth(username, password)

                // Further modifications are possible here, for example:
                // pr.Out.Header.Set("X-Dogu-API-Version", "2")
            },
            ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
                http.Error(w, "Dogu unreachable", http.StatusBadGateway)
            },
        }
        proxy.ServeHTTP(w, r)
    })

    server := &http.Server{
        Addr: "127.0.0.1:8081",
        Handler: handler,
        ReadHeaderTimeout: 10 * time.Second,
    }
    log.Fatal(server.ListenAndServe())
}
```

The example can be saved as `adapter.go` outside the proxy project and started
with `go run adapter.go`. It uses only the Go standard library. Do not place it
in the root directory of this repository as a second file with its own `main`
function, because it would conflict with the existing `main` function there.

The example adapter removes cookies so that a session sent with the request does
not override the authentication derived from the PAT. Whether this is necessary
for a particular Dogu depends on its authentication behavior. It also does not
preserve incoming forwarding headers; if the Dogu needs this metadata, the
adapter must implement an appropriate trust contract for it.

### Testing your own adapter

The following cases are particularly useful for integration testing:

- Valid PAT: CAS returns `200`, the adapter receives `cas-pat` and the correct
  decoded username; the Dogu receives the credentials derived from that identity.
- Invalid PAT: CAS returns `401`; neither the adapter nor the Dogu is called.
- CAS outage: The client receives `502`; no forwarding takes place.
- Internal headers set by the client: The PAT proxy removes them before they
  reach the adapter.
- Request without a PAT: It reaches the adapter without an internal identity
  and follows the adapter's explicitly defined handling policy.
- Unknown Dogu user: The adapter rejects the request instead of using a shared
  privileged user.
- Paths, query parameters, and bodies: The intended forwarding also works with
  URL-encoded characters, repeated parameters, and larger amounts of data.

## Operation and troubleshooting

### Logs and error responses

The proxy writes structured JSON logs to standard error (`stderr`). Request-related
events contain a random `requestId` and the time elapsed since the start of the
request, `elapsedMs`. These logs are not a complete access log of all successful
upstream requests.

Key events are:

| Event | Meaning |
| --- | --- |
| `starting` | The proxy is starting the listener. |
| `cas_validation_ok` | CAS accepted the PAT with `200`. |
| `cas_validation_rejected` | CAS returned a different HTTP status. |
| `cas_validation_error` | The CAS request failed, for example with `CAS_TIMEOUT` or `CAS_CONNECTION_ERROR`. |
| `upstream_connection_error` | Forwarding to the upstream failed. |
| `upstream_response` | The upstream returned a status of `400` or higher. |

Errors generated by the proxy during CAS validation or forwarding include
`Cache-Control: no-store` and `X-Pat-Proxy-Request-Id`. The identifier helps match
the response to its log entry. Ordinary upstream responses do not automatically
receive this header. If even the random identifier cannot be generated, the
proxy responds with `503 Service Unavailable`.

### Common causes

| Observation | What to check |
| --- | --- |
| Proxy does not start | Port availability, listen address, and the configuration rules described above. A CAS URL is required when validation is enabled. |
| `502` with a CAS error in the log | Check the complete validation endpoint, reachability, certificate trust, and timeout. CAS redirects or CAS `403` responses also result in `502`. |
| `502` with `upstream_connection_error` | Is the adapter running? Are the host and port, or the socket path and access permissions, correct? |
| No CAS validation in the log | Does the Basic Auth password start with `pat_`? Does the path match `apiPath`? Is validation enabled? |
| Adapter reports a missing identity | Was the request successfully validated as a PAT request at all? Unvalidated requests do not receive internal authentication headers. |
| Dogu rejects a request that CAS considers valid | Check user mapping, Dogu credentials, and permissions in the adapter or Dogu. |

### Shutdown and timeouts

`Ctrl+C` (`SIGINT`) or `SIGTERM` shuts down the proxy gracefully. Active requests
are given up to five seconds to finish. Any connections still open after that
are closed, and the process reports an error.

The HTTP server allows ten seconds for reading request headers and 60 seconds
for idle keep-alive connections. `validationTimeout` limits only the CAS request;
it is not a general timeout for the complete Dogu request. Connections to a
Unix socket have a connection establishment timeout of five seconds.

## Navigating the source code

- `main.go`: CLI application, JSON logger, and program startup.
- `config.go`: Options, environment variables, defaults, and startup validation.
- `proxy.go`: HTTP server, PAT detection, CAS requests, internal headers,
  forwarding, and graceful shutdown.
- `Makefile` and `build/make/`: Building and packaging.

The key functions are `patUsername` for PAT detection, `validate` for the CAS
request, `stripInternalHeaders` for sanitization, and `ServeHTTP` for processing
and forwarding a request.
