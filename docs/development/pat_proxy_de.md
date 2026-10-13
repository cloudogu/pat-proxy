# PAT-Proxy: Funktion, Konfiguration und Upstream-Adapter

## Wofür ist der Proxy da?

Ein Personal Access Token (PAT) ist ein persönlicher Zugangsschlüssel. Ein Client
kann damit eine API aufrufen, ohne bei jedem Aufruf eine interaktive Anmeldung
durchzuführen. Der PAT-Proxy prüft solche Schlüssel beim Central Authentication
Service (CAS) und leitet erlaubte Anfragen an einen konfigurierten HTTP-Dienst
weiter. Im Cloudogu-Umfeld ist dieser Dienst ein Dogu oder ein vorgeschalteter
Adapter.

Der Proxy übernimmt die Prüfung des PAT. Ein Upstream-Adapter übersetzt die
bestätigte Benutzeridentität in die Anmeldung, die das jeweilige Dogu versteht.
Beispielsweise kann er einen lokalen API-Schlüssel einsetzen oder ein
dienstspezifisches Token beschaffen.

```text
Client ──HTTP──> PAT-Proxy ──HTTP──> Upstream-Adapter ──HTTP──> Dogu/API
                    │
                    └──GET mit Basic Auth──> CAS: PAT prüfen
```

„Upstream“ bezeichnet hier den nächsten Dienst, an den der Proxy eine Anfrage
weiterleitet. Ein Adapter ist optional, wenn das Dogu den internen
Authentifizierungsvertrag des Proxys bereits selbst unterstützt. Der Proxy
erstellt keine Dogu-Tokens und implementiert keine dienstspezifische Anmeldung.

## Wie wird eine Anfrage verarbeitet?

### 1. Interne Header entfernen

Der Proxy entfernt aus jeder eingehenden Anfrage alle Header, deren Name mit
`X-Internal-Auth-` beginnt. Groß- und Kleinschreibung spielen dabei keine Rolle.
Damit kann ein Client nicht einfach einen Benutzernamen in diesem Header angeben
und dadurch eine erfolgreiche PAT-Prüfung vortäuschen.

### 2. PAT-Anfragen erkennen

Eine Anfrage wird nur dann bei CAS geprüft, wenn alle folgenden Bedingungen
erfüllt sind:

- Die Validierung ist aktiviert; das ist die Standardeinstellung.
- Der Pfad liegt im konfigurierten API-Bereich.
- Der `Authorization`-Header enthält gültig kodierte HTTP Basic Auth mit einem
  Benutzernamen und einem Passwort.
- Das Passwort beginnt exakt mit `pat_`.

Ein Client überträgt den PAT also als Basic-Auth-Passwort, zum Beispiel:

```bash
curl --user 'alice:pat_BEISPIEL' http://127.0.0.1:8080/api/projects
```

Bei `--apiPath /api` passen `/api` und `/api/projects`, aber nicht `/apix`.
`--apiPath /` erfasst alle Pfade. Für einen API-Bereich empfiehlt sich die
Schreibweise ohne abschließenden Schrägstrich. Die Erkennung verwendet den
URL-kodierten Pfad; sie normalisiert oder dekodiert ihn nicht vorher.

Andere Anfragen werden ohne CAS-Prüfung weitergeleitet. Das gilt etwa für normale
Basic-Auth-Passwörter, Bearer-Tokens, Anfragen ohne Zugangsdaten und Pfade außerhalb
des API-Bereichs. Auch eine fehlerhafte Basic-Auth-Kodierung löst keine PAT-Prüfung
aus. Die Anwendung oder der Adapter muss diese Anfragen selbst behandeln. Der
Proxy erzwingt also nicht, dass jede Anfrage einen PAT enthält.

### 3. Den PAT bei CAS prüfen

Für jede erkannte PAT-Anfrage sendet der Proxy einen eigenen `GET` an die vollständig
konfigurierte CAS-Validierungs-URL. Er übernimmt dabei den ursprünglichen
`Authorization`-Header. Der Body der Client-Anfrage wird nicht an CAS gesendet.
Das Ergebnis wird nicht zwischengespeichert: Auch derselbe PAT wird bei einem
weiteren Aufruf erneut geprüft.

Ein konfigurierter Scope wird als URL-kodierter Query-Parameter `scope` angehängt.
Zum Beispiel wird aus `--scope /mydogu` der Parameter `scope=%2Fmydogu`. Bereits
vorhandene `scope`-Parameter werden ersetzt; andere Query-Parameter bleiben
inhaltlich erhalten. Ohne Scope-Option bleibt die konfigurierte URL unverändert,
einschließlich eines dort bereits eingetragenen Scopes.

| Ergebnis der CAS-Abfrage | Verhalten des PAT-Proxys |
| --- | --- |
| HTTP `200` | Anfrage an den Upstream weiterleiten. |
| HTTP `400` | Client erhält `400 Bad Request`; keine Weiterleitung. |
| HTTP `401` | Client erhält `401 Unauthorized`; keine Weiterleitung. |
| Jeder andere Status, auch `403`, `404`, `500` oder ein Redirect | Client erhält `502 Bad Gateway`; keine Weiterleitung. |
| Verbindungsfehler, TLS-Fehler oder Zeitüberschreitung | Client erhält `502 Bad Gateway`; keine Weiterleitung. |

Redirects werden nicht verfolgt. Der CAS-Antwortbody wird nicht ausgewertet oder
an den Client weitergereicht. Die Identität stammt aus dem Basic-Auth-Benutzernamen
der Anfrage, nachdem CAS diese Zugangsdaten mit HTTP `200` akzeptiert hat.

### 4. Die geprüfte Identität an den Upstream übergeben

Nach erfolgreicher Prüfung entfernt der Proxy den `Authorization`-Header und setzt:

```http
X-Internal-Auth-Method: cas-pat
X-Internal-Auth-User: YWxpY2U=
```

`YWxpY2U=` ist der mit Standard-Base64 kodierte UTF-8-Benutzername `alice`.
Der Adapter erhält damit die bestätigte Identität, aber nicht den ursprünglichen
PAT über den Authorization-Header.

Bei Anfragen ohne erfolgreiche PAT-Prüfung setzt der Proxy diese beiden Header
nicht. Der ursprüngliche `Authorization`-Header bleibt dann erhalten. Auch bei
deaktivierter Validierung werden eingehende interne Authentifizierungsheader
entfernt, während die normalen Zugangsdaten weitergereicht werden.

### 5. Die Anfrage weiterleiten und die Antwort zurückgeben

Der Proxy setzt das Verbindungsziel auf den konfigurierten Upstream. Methode,
Body, Pfad einschließlich kodierter Pfadbestandteile, Query und ursprünglicher
HTTP-Host bleiben erhalten. Der HTTP-Host kann also vom Hostnamen des
Verbindungsziels abweichen.

Die Werte von `X-Forwarded-For`, `X-Forwarded-Host` und `X-Forwarded-Proto` werden
übernommen, wenn sie vorhanden sind. Der Proxy erzeugt diese Angaben nicht selbst.
Ein vorgeschalteter vertrauenswürdiger Proxy sollte sie bereinigen oder setzen,
bevor sie für Sicherheitsentscheidungen verwendet werden.

Die normale HTTP-Reverse-Proxy-Verarbeitung entfernt verbindungsbezogene Header,
etwa `Connection`; eine bytegenaue Kopie aller Header ist daher nicht zugesichert.
Upstream-Antworten werden an den Client weitergegeben, auch Fehlerantworten.
Kann die Weiterleitung nicht durchgeführt werden, liefert der Proxy `502`.

## Bauen und starten

Alle folgenden Build-Befehle werden im Stammverzeichnis dieses Repositorys
ausgeführt. Das Projekt verlangt laut `go.mod` Go **1.26.0**.

### Mit Make und Docker bauen

Für den lokalen Make-Build werden Make und Docker mit einem erreichbaren
Docker-Daemon benötigt. Der Build verwendet das Image `golang:1.26.0`.

```bash
make
```

Das Programm liegt danach unter `target/pat-proxy`. Zusätzlich erstellt Make
ein Tar-Archiv im Verzeichnis `target`. Der CLI-Anwendungsname `patproxy` ist
nicht der Name des von Make erzeugten Programms.

### Direkt mit Go bauen

Mit einer passenden lokalen Go-Installation lässt sich auch ohne Docker bauen.
Die Abhängigkeiten sind im Verzeichnis `vendor` enthalten:

```bash
mkdir -p target
go build -mod=vendor -o target/pat-proxy .
```

### Mit PAT-Prüfung starten

Zuerst müssen der Upstream und CAS erreichbar sein. Die Namen und der Scope im
folgenden Beispiel müssen an die eigene Umgebung angepasst werden:

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

Der Client verbindet sich mit Port `8080`; der Proxy leitet an Port `8081`
weiter. `/cas/api/pats/validate` ist ein Beispiel für einen Validierungspfad:
Einzutragen ist der tatsächliche vollständige Endpunkt der eigenen CAS-Instanz,
nicht nur deren Basisadresse.

### Lokal ohne PAT-Prüfung starten

Für einen lokalen Versuch mit einem bereits laufenden Upstream:

```bash
./target/pat-proxy -la 127.0.0.1:8080 -uh 127.0.0.1 -up 8081 \
  --validationEnabled=false
```

Dieser Modus prüft keine Zugangsdaten und erstellt keine interne
Benutzeridentität. Der Upstream muss die übergebenen Zugangsdaten selbst
verarbeiten. Ohne erreichbaren Upstream liefert ein Aufruf HTTP `502`.

### Einen Unix-Socket verwenden

Statt einer TCP-Verbindung kann der Proxy einen vom Adapter bereitgestellten
Unix-Socket verwenden:

```bash
./target/pat-proxy \
  -la 127.0.0.1:8080 \
  -us /run/mydogu/adapter.sock \
  -api /api \
  -cas 'https://cas.example.org/cas/api/pats/validate' \
  -s /mydogu
```

Bei gesetztem Socket-Pfad erfolgt die Verbindung über diesen Socket; Host und
Port bestimmen dann nicht den Netzwerktransport. Der Adapter muss darüber
HTTP sprechen. Der Socket und sein übergeordnetes Verzeichnis müssen für den
Proxy-Prozess zugänglich sein. Die Portkonfiguration wird beim Start trotzdem
auf einen gültigen Wert geprüft.

## Konfiguration im Detail

Die Konfiguration erfolgt über CLI-Optionen oder die unten aufgeführten
Umgebungsvariablen. Eine Konfigurationsdatei wird derzeit nicht eingelesen.
CLI-Werte haben Vorrang vor Umgebungsvariablen; danach gelten die Standardwerte.

| Langform | Kurzform | Umgebungsvariable | Standard | Bedeutung |
| --- | --- | --- | --- | --- |
| `--listenAddress` | `-la` | `PAT_PROXY_LISTEN_ADDRESS` | `0.0.0.0:8080` | Adresse und Port, auf denen der Proxy HTTP annimmt. |
| `--upstreamHost` | `-uh` | `PAT_PROXY_UPSTREAM_HOST` | `127.0.0.1` | Hostname oder IP-Adresse des Upstreams. |
| `--upstreamPort` | `-up` | `PAT_PROXY_UPSTREAM_PORT` | `8081` | TCP-Port des Upstreams. |
| `--upstreamSocket` | `-us` | `PAT_PROXY_UPSTREAM_SOCKET` | leer | Unix-Socket statt TCP verwenden. |
| `--apiPath` | `-api` | `PAT_PROXY_API_PATH` | `/` | Pfadbereich, in dem PAT-Anfragen erkannt werden. |
| `--casUrl` | `-cas` | `PAT_PROXY_CAS_URL` | leer | Vollständige HTTP(S)-URL zur PAT-Prüfung. |
| `--scope` | `-s` | `PAT_PROXY_SCOPE` | leer | Scope für die CAS-Abfrage. |
| `--validationTimeout` | `-t` | keine | `5` | Zeitlimit der CAS-Abfrage in Sekunden. |
| `--validationEnabled` | keine | `PAT_PROXY_VALIDATION_ENABLED` | `true` | PAT-Prüfung aktivieren oder deaktivieren. |
| `--casInsecure` | keine | `PAT_PROXY_CAS_INSECURE` | `false` | TLS-Zertifikatsprüfung ausschließlich für CAS abschalten. |

Beispiel mit Umgebungsvariablen:

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

Für `validationTimeout` existiert keine Umgebungsvariable. Eine frei erfundene
Variable wie `PAT_PROXY_VALIDATION_TIMEOUT` hätte daher keine Wirkung.

Beim Start werden folgende Bedingungen geprüft:

- `apiPath` ist nicht leer und beginnt mit `/`.
- `listenAddress` enthält Host und Port; zum Beispiel `127.0.0.1:8080` oder `:8080`.
- Listen-Port und Upstream-Port liegen zwischen `1` und `65535`.
- `validationTimeout` ist größer als `0`, auch bei deaktivierter Validierung.
- Bei aktiver Validierung ist `casUrl` eine HTTP(S)-URL mit Hostnamen, ohne
  eingebettete Zugangsdaten und ohne Fragment wie `#abschnitt`.

Eine ungültige Konfiguration verhindert den Start; das Programm endet mit
Exit-Code `1`. Alle Optionen lassen sich über `./target/pat-proxy --help` anzeigen.

Der Listener und die Upstream-Verbindung verwenden HTTP. TLS für eingehende
Verbindungen muss bei Bedarf ein vorgeschalteter Dienst übernehmen. Für CAS
unterstützt der Proxy HTTPS mit mindestens TLS 1.2.
`--casInsecure` ist für eine Umgebung mit bewusst ungeprüften Zertifikaten gedacht;
bei regulärem Betrieb sollte die Zertifikatsprüfung aktiv bleiben.

Der Upstream-Transport ignoriert HTTP-Proxy-Umgebungsvariablen. Der separate
CAS-Transport behält dagegen das Standardverhalten von Go bei und kann solche
Variablen berücksichtigen.

## Einen Upstream-Adapter implementieren

### Architektur und Aufgaben

Im Repository gibt es keine Adapter-Registry, kein Adapter-Interface und keine
Plugin-Ladefunktion. Ein Adapter ist ein eigenständiger HTTP-Dienst, auf den
`upstreamHost` und `upstreamPort` oder `upstreamSocket` zeigen. Er kann in Go oder
einer anderen Sprache mit HTTP-Unterstützung implementiert werden.

Ein typischer Adapter erledigt diese Schritte:

1. Eine Anfrage vom PAT-Proxy annehmen.
2. Prüfen, ob `X-Internal-Auth-Method` den Wert `cas-pat` hat.
3. `X-Internal-Auth-User` mit Standard-Base64 dekodieren und den Benutzernamen
   nach den Regeln des Zielsystems prüfen.
4. Die bestätigte CAS-Identität einem Benutzer des Dogus zuordnen.
5. Die für das Dogu nötigen Zugangsdaten ermitteln oder erzeugen.
6. Die Anfrage anpassen und an das Dogu weiterleiten.
7. Die Antwort an den PAT-Proxy zurückgeben.

Der interne Authentifizierungsheader ist nur auf einem geschützten Transport
vertrauenswürdig. Base64 ist eine Kodierung, keine Verschlüsselung oder Signatur.
Der Adapter sollte deshalb nur über eine interne Verbindung erreichbar sein,
etwa einen Unix-Socket mit passenden Dateirechten oder eine abgeschottete
Netzwerkverbindung. Loopback allein schützt nicht vor anderen lokalen Prozessen.
Kann ein Client den Adapter direkt erreichen, könnte er die beiden Header selbst
setzen und die CAS-Prüfung umgehen.

Anfragen ohne interne Identität brauchen eine ausdrücklich festgelegte Behandlung.
Ein Adapter kann sie beispielsweise unverändert zur normalen Anmeldung des Dogus
weitergeben oder mit `401` ablehnen. Diese Entscheidung hängt davon ab, ob der
Adapter nur PAT-Zugriffe oder auch andere Zugriffe bedienen soll.

Die CAS-Prüfung ersetzt nicht die Berechtigungsprüfung im Dogu. Der Adapter darf
einen bestätigten Benutzer nicht automatisch mit unbegrenzten Rechten ausstatten.

### Welche Teile des Requests darf ein Adapter verändern?

Als eigenständiger Dienst kann der Adapter alle Teile der Anfrage bearbeiten.
Der PAT-Proxy bietet dafür keine besonderen Konfigurationsoptionen; die Änderungen
werden im Adapter programmiert.

| Teil der Anfrage | Mögliche Änderung | Worauf achten? |
| --- | --- | --- |
| Authentifizierung | Dogu-spezifischen Bearer-Token, Basic Auth oder Session-Cookie einsetzen. | Die Zugangsdaten müssen zur geprüften Identität und deren Rechten passen. |
| Header | API-Schlüssel setzen, benötigte Header ergänzen oder fremde Authentifizierungsheader entfernen. | Interne Identitätsheader vor dem Aufruf des Dogus entfernen, sofern das Dogu sie nicht selbst benötigt. |
| Ziel und HTTP-Host | Internen Dogu-Endpunkt wählen und den `Host`-Header passend setzen. | Ziele kontrolliert auswählen; Client-Eingaben dürfen keine beliebigen Verbindungsziele bestimmen. |
| Pfad | `/api/projects` zum Beispiel auf `/internal/api/projects` abbilden. | Kodierte Zeichen, Unterpfade und Schrägstriche bewusst behandeln. |
| Query-Parameter | Parameter ergänzen, ersetzen oder entfernen. | Mehrfachwerte und URL-Kodierung korrekt verarbeiten. |
| HTTP-Methode | Eine Methode für die Ziel-API übersetzen. | Bedeutung, Nebenwirkungen und Wiederholbarkeit der Anfrage berücksichtigen. |
| Body | JSON umformen oder zusätzliche Felder einfügen. | Größe begrenzen; `Content-Type` und `Content-Length` anpassen. Streaming nicht unbeabsichtigt durch vollständiges Einlesen verlieren. |

Auch Antworten kann der Adapter anpassen, etwa Antwortheader, Cookies oder
Redirect-Adressen. Wenn er einen Antwortbody umschreibt, muss er ebenfalls Länge
und gegebenenfalls Komprimierung berücksichtigen.

### Beispiel: Ein kleiner Go-Adapter

Das folgende Beispiel zeigt den HTTP-Vertrag und die Bearbeitung vor der
Weiterleitung. Der Adapter nimmt ausschließlich geprüfte PAT-Anfragen an,
dekodiert den Benutzernamen, setzt Basic Auth für das Zielsystem und entfernt
die internen Header. Er lauscht auf `127.0.0.1:8081` und leitet an einen
beispielhaften Dogu-Dienst auf `127.0.0.1:8082` weiter.

Die Funktion `doguPasswordFor` ist bewusst nur ein Platzhalter: Sie liefert
ausschließlich für den Beispielbenutzer `alice` ein festes Testpasswort.
Ein produktiver Adapter muss hier die tatsächliche Benutzerzuordnung und
Zugangsdatenbeschaffung implementieren. Das Passwort gehört dann beispielsweise
in einen geschützten Secret-Speicher und nicht in den Quellcode.

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
    return "NUR-EIN-TESTPASSWORT", true
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
            http.Error(w, "Gepruefte PAT-Identitaet erforderlich", http.StatusUnauthorized)
            return
        }

        decoded, err := base64.StdEncoding.DecodeString(
            r.Header.Get("X-Internal-Auth-User"),
        )
        if err != nil || len(decoded) == 0 {
            http.Error(w, "Ungueltige interne Identitaet", http.StatusUnauthorized)
            return
        }
        username := string(decoded)
        password, ok := doguPasswordFor(username)
        if !ok {
            http.Error(w, "Kein Dogu-Zugang fuer diesen Benutzer", http.StatusForbidden)
            return
        }

        proxy := &httputil.ReverseProxy{
            Transport: transport,
            Rewrite: func(pr *httputil.ProxyRequest) {
                pr.SetURL(target)
                pr.Out.Host = target.Host
                removeInternalHeaders(pr.Out.Header)

                // Anmeldung fuer das Dogu aus der bestaetigten Identitaet ableiten.
                pr.Out.Header.Del("Authorization")
                pr.Out.Header.Del("Cookie")
                pr.Out.SetBasicAuth(username, password)

                // Hier sind weitere Anpassungen moeglich, zum Beispiel:
                // pr.Out.Header.Set("X-Dogu-API-Version", "2")
            },
            ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
                http.Error(w, "Dogu nicht erreichbar", http.StatusBadGateway)
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

Das Beispiel kann außerhalb des Proxy-Projekts als `adapter.go` gespeichert und
mit `go run adapter.go` gestartet werden. Es verwendet nur die Go-Standardbibliothek.
Nicht als zweite Datei mit eigener `main`-Funktion in das Stammverzeichnis dieses
Repositorys legen, weil sie dort mit der vorhandenen `main`-Funktion kollidiert.

Der Beispieladapter entfernt Cookies, damit eine mitgeschickte Sitzung nicht die
aus dem PAT abgeleitete Anmeldung überlagert. Ob das für ein bestimmtes Dogu nötig
ist, muss dessen Authentifizierungsverhalten entscheiden. Er übernimmt auch keine
eingehenden Forwarding-Header; falls das Dogu solche Metadaten benötigt, muss der
Adapter dafür einen passenden Vertrauensvertrag implementieren.

### Einen eigenen Adapter überprüfen

Für die Integration sind besonders diese Fälle hilfreich:

- Gültiger PAT: CAS liefert `200`, der Adapter erhält `cas-pat` und den richtigen
  dekodierten Benutzernamen; das Dogu erhält die daraus abgeleiteten Zugangsdaten.
- Ungültiger PAT: CAS liefert `401`; weder Adapter noch Dogu werden aufgerufen.
- Ausfall von CAS: Der Client erhält `502`; es erfolgt keine Weiterleitung.
- Vom Client gesetzte interne Header: Der PAT-Proxy entfernt sie, bevor sie den
  Adapter erreichen.
- Anfrage ohne PAT: Sie erreicht den Adapter ohne interne Identität und folgt
  dessen ausdrücklich festgelegter Behandlung.
- Unbekannter Dogu-Benutzer: Der Adapter lehnt die Anfrage ab, statt einen
  gemeinsamen privilegierten Benutzer einzusetzen.
- Pfade, Query-Parameter und Bodies: Die gewünschte Weiterleitung funktioniert
  auch mit URL-kodierten Zeichen, Mehrfachparametern und größeren Datenmengen.

## Betrieb und Fehlersuche

### Logs und Fehlerantworten

Der Proxy schreibt strukturierte JSON-Logs nach Standardfehlerausgabe (`stderr`).
Anfragebezogene Ereignisse enthalten eine zufällige `requestId` und die seit
Anfragebeginn verstrichene Zeit `elapsedMs`. Es handelt sich nicht um ein
vollständiges Access-Log aller erfolgreichen Upstream-Aufrufe.

Wichtige Ereignisse sind:

| Ereignis | Bedeutung |
| --- | --- |
| `starting` | Der Proxy startet den Listener. |
| `cas_validation_ok` | CAS hat den PAT mit `200` akzeptiert. |
| `cas_validation_rejected` | CAS hat einen anderen HTTP-Status geliefert. |
| `cas_validation_error` | Die CAS-Anfrage ist fehlgeschlagen, etwa mit `CAS_TIMEOUT` oder `CAS_CONNECTION_ERROR`. |
| `upstream_connection_error` | Die Weiterleitung an den Upstream ist fehlgeschlagen. |
| `upstream_response` | Der Upstream hat einen Status ab `400` geliefert. |

Vom Proxy erzeugte Fehler bei CAS-Prüfung oder Weiterleitung enthalten
`Cache-Control: no-store` und `X-Pat-Proxy-Request-Id`. Die Kennung hilft, die
Antwort dem Logeintrag zuzuordnen. Normale Upstream-Antworten bekommen diesen
Header nicht automatisch. Kann bereits die zufällige Kennung nicht erzeugt
werden, antwortet der Proxy mit `503 Service Unavailable`.

### Häufige Ursachen

| Beobachtung | Was prüfen? |
| --- | --- |
| Proxy startet nicht | Portbelegung, Listen-Adresse und die oben beschriebenen Konfigurationsregeln. Bei aktiver Prüfung ist eine CAS-URL erforderlich. |
| `502` mit CAS-Fehler im Log | Vollständigen Validierungsendpunkt, Erreichbarkeit, Zertifikatsvertrauen und Timeout prüfen. Auch CAS-Redirects oder CAS-`403` führen zu `502`. |
| `502` mit `upstream_connection_error` | Läuft der Adapter? Stimmen Host und Port beziehungsweise Socket-Pfad und Zugriffsrechte? |
| Keine CAS-Prüfung im Log | Beginnt das Basic-Auth-Passwort mit `pat_`? Passt der Pfad zu `apiPath`? Ist die Validierung aktiviert? |
| Adapter meldet fehlende Identität | Wurde die Anfrage überhaupt erfolgreich als PAT-Anfrage geprüft? Ungeprüfte Anfragen bekommen keine internen Authentifizierungsheader. |
| Dogu lehnt eine bei CAS gültige Anfrage ab | Benutzerzuordnung, Dogu-Zugangsdaten und Berechtigungen im Adapter beziehungsweise Dogu prüfen. |

### Beenden und Zeitlimits

Mit `Ctrl+C` (`SIGINT`) oder `SIGTERM` wird der Proxy kontrolliert beendet.
Laufende Anfragen haben dafür bis zu fünf Sekunden Zeit. Danach werden noch
offene Verbindungen geschlossen und der Prozess meldet einen Fehler.

Der HTTP-Server setzt zehn Sekunden zum Lesen der Request-Header und
60 Sekunden für ungenutzte Keep-alive-Verbindungen. `validationTimeout`
begrenzt ausschließlich die CAS-Abfrage; es ist kein allgemeines Zeitlimit für
den vollständigen Aufruf des Dogus. Für die Verbindung zu einem Unix-Socket
gilt ein Verbindungsaufbau-Timeout von fünf Sekunden.

## Orientierung im Quellcode

- `main.go`: CLI-Anwendung, JSON-Logger und Programmstart.
- `config.go`: Optionen, Umgebungsvariablen, Standardwerte und Startvalidierung.
- `proxy.go`: HTTP-Server, PAT-Erkennung, CAS-Abfrage, interne Header,
  Weiterleitung und kontrolliertes Herunterfahren.
- `Makefile` und `build/make/`: Build und Paketierung.

Die entscheidenden Funktionen sind `patUsername` für die PAT-Erkennung,
`validate` für die CAS-Abfrage, `stripInternalHeaders` für die Bereinigung und
`ServeHTTP` für die Verarbeitung und Weiterleitung einer Anfrage.
