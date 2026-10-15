# pat-proxy
Proxy-Application to handle PAT-Requests for OIDC Dogus

## Bauen und starten

```sh
make
./target/patproxy -la :8080 -uh 127.0.0.1 -up 8081 --validationEnabled=false
```

Dieser Start deaktiviert die PAT-Prüfung explizit. Ohne erreichbaren Upstream
antwortet der Proxy mit HTTP 502.

Mit PAT-Prüfung (standardmäßig aktiviert):

```sh
./target/patproxy -la :8080 -uh 127.0.0.1 -up 8081 -api /api \
  -cas 'https://cas.example.org/cas/api/pats/validate' -s /doguname -t 5
```

Die CAS-Adresse muss eine vollständige Validierungs-URL sein.
`-s` beziehungsweise `--scope` hängt den Scope URL-kodiert als letzten
Query-Parameter an. Andere Query-Parameter bleiben erhalten; ein vorhandener
Scope wird ersetzt. Ohne Scope-Option bleibt die URL unverändert.
`-us /pfad/adapter.sock` verwendet einen Unix-Socket statt TCP.
`--help` zeigt alle Optionen.

| Kurzform | Langform | Standard |
| --- | --- | --- |
| `-la` | `--listenAddress` | `0.0.0.0:8080` |
| `-uh` | `--upstreamHost` | `127.0.0.1` |
| `-up` | `--upstreamPort` | `8081` |
| `-us` | `--upstreamSocket` | leer |
| `-api` | `--apiPath` | `/` |
| `-cas` | `--casUrl` | leer, bei aktiver Prüfung erforderlich |
| `-s` | `--scope` | leer |
| `-t` | `--validationTimeout` | 5 Sekunden, muss positiv sein |
| | `--validationEnabled` | `true` |
| | `--casInsecure` | `false` |

Alternativ werden `PAT_PROXY_LISTEN_ADDRESS`, `PAT_PROXY_UPSTREAM_HOST`,
`PAT_PROXY_UPSTREAM_PORT`, `PAT_PROXY_UPSTREAM_SOCKET`, `PAT_PROXY_API_PATH`,
`PAT_PROXY_CAS_URL`, `PAT_PROXY_SCOPE`, `PAT_PROXY_VALIDATION_ENABLED` und `PAT_PROXY_CAS_INSECURE`
unterstützt. CLI-Werte haben Vorrang.

## Verhalten

Basic-Auth-Passwörter mit Präfix `pat_` im konfigurierten API-Pfad und dessen
Unterpfaden werden per GET bei CAS geprüft. Nur HTTP 200 erlaubt die Weiterleitung.
CAS 400/401 werden zurückgegeben; andere Antworten, Redirects, Timeouts und
Verbindungsfehler liefern HTTP 502. Ergebnisse werden nicht gecacht.

Eingehende `X-Internal-Auth-*`-Header werden entfernt. Nach erfolgreicher Prüfung
wird Authorization entfernt und durch `X-Internal-Auth-Method: cas-pat` sowie
`X-Internal-Auth-User` (Base64-Benutzername) ersetzt. Der Upstream muss diesen
Vertrag unterstützen und über einen geschützten internen Transport erreichbar
sein; Base64 ist keine Signatur. Ohne Prüfung werden Zugangsdaten weitergereicht.

Methode, Body, Pfad, Query und Host bleiben erhalten.

SIGINT/SIGTERM erlauben bis zu fünf Sekunden zum Abschließen laufender Anfragen.

