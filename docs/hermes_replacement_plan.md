# Plan: Mattermost-Agent als Hermes-Ersatz mit gemeinsamem Graphiti-Memory

**Archiviert:** Der Nutzer hat am 22.09.2026 entschieden, keinen Custom Mattermost Plugin mehr zu betreiben. Plugin-Ausbau und Hermes-Off-Cutover nach diesem Plan werden nicht fortgesetzt. Der tatsächliche Rückbau und der gesicherte Entwicklungsstand stehen in [Stand und Rückbau](stand_und_rueckbau_2026-09-22.md).

Stand: 2026-09-22. Dieses Dokument ist ein Arbeitsplan, keine Freigabe für Installation oder Abschaltung.

## Ziel und geprüfter Ausgangspunkt

Ein Bot reagiert in jedem Mattermost-Channel, dem er als Mitglied hinzugefügt wurde, direkt im Channel. Seine laufende Codex-Session ist pro Channel und Bot geteilt. Beim ersten Turn erhält sie höchstens 30 frühere Posts beziehungsweise 12.000 Zeichen als Startkontext. Der Live-Test im Channel `Codex Hermes-Off Cloud Test` bestätigte eine Antwort ohne Erwähnung, als sichtbaren Channel-Post und die Verwendung einer früheren Channel-Nachricht.

Der Bot nutzt einen Codex-App-Server mit eigenem `CODEX_HOME` im Mattermost-Container. In dieser Runtime sind derzeit keine MCP-Server eingetragen; die MCP-Konfiguration der Pi-Instanz wird nicht übernommen. Der Hermes-Off-Auto-Finalizer meldete am 2026-09-22 `ready=false`, `infraReady=false`, `hermesAdapterReady=false` und nur zwei erfolgreiche Soak-Tage (2026-08-20 und 2026-08-21). `productionChannelsReady=true`. Der Preflight läuft noch auf `centralserver` und sucht dort den Mattermost-Container sowie den Voice-Adapter an der alten Host-Bridge-Adresse. Mattermost läuft inzwischen auf `nas1`; dadurch sind diese Negativbefunde für den aktuellen Betrieb nicht aussagekräftig. Die n8n-Zählungen kommen außerdem als `unknown` zurück und machen die Hermes-Adapter-Prüfung rot.

Graphiti ist derzeit nicht als MCP in der Pi-Codex-Liste eingetragen. Ob anderswo schon eine Graphiti-Instanz existiert, ist noch zu prüfen. Ziel ist **ein gemeinsames, dauerhaftes Memory für alle angeschlossenen Agenten und Channels**, nicht je ein Graph oder `group_id` pro Channel. Der Gesprächskontext der Codex-Session bleibt dagegen pro Channel und Bot getrennt.

## 1. MCP-Werkzeuge für die Channel-Runtime

- [x] Pi-Codex-MCP-Namen inventarisiert: `mattermost`, `qnap-container-station-nas1`, `telegram-mcp`, `unifi-access`, `unifi-network`, `unifi-protect`, `uptime_kuma`, `QNAP_NAS`, `borg-backup-ui`, `homeassistant`, `komodo`, `n8n` (Dev), `n8n-prod`, `psono`. Das ist eine Host-Inventur, keine Freigabe für den Channel-Bot.
- [ ] Benötigte Hermes-Funktionen und einzelne Tools auswählen. Für jedes Tool festhalten: lesend oder schreibend, Zugangsdaten, Besitzer, erlaubte Channels/Nutzer und Freigabeverhalten. `psono` wegen Secret-Zugriff, `n8n-prod` wegen Produktionswirkung sowie Host-/Netzwerk-Administrationswerkzeuge nicht pauschal in die Bot-Runtime übernehmen.
- [ ] Festlegen, wie der Codex-App-Server im Mattermost-Container die freigegebenen MCP-Server erreicht. Die bestehende Plugin-MCP-Konfiguration allein stellt diese Tools der getrennten Codex-Runtime nicht bereit.
- [ ] Konfiguration und Secrets nur für die benötigten Server bereitstellen; keine pauschale Kopie der persönlichen Pi-Codex-Konfiguration. Netzwerkzugang, Berechtigungsgrenzen und Tool-Freigaben pro Bot/Channel testen.
- [ ] Je ein reales Lese- und Schreibszenario inklusive Ablehnung/Freigabe durchspielen; Tool-Aufrufe und Ergebnisse nachvollziehbar protokollieren, ohne Inhalte oder Secrets in Audit-Records zu schreiben.

**Fertig, wenn:** Der Channel-Bot die festgelegten Hermes-Werkzeuge zuverlässig nutzen kann und ein Bot in einem nicht freigegebenen Channel keinen Zugriff erhält.

## 2. Gemeinsames Langzeitgedächtnis mit Graphiti

### Architekturentscheidung

Alle angeschlossenen Agenten (zunächst Mattermost, später zum Beispiel Coding- oder Home-Assistant-Agent) verwenden **denselben logischen Wissensgraphen**. Graphitis vorhandener MCP-Server übernimmt das Speichern, Suchen und Löschen von Episoden und Fakten. Für den isolierten Pilot mit unkritischen Beispieldaten nutzen wir diese vorhandenen Tools direkt. Erst für echte Channel-Daten ist eine schlanke, agentenunabhängige Kontrollschicht nötig: Sie entscheidet über selektives Speichern, Herkunft, Berechtigungen und Korrektur und reicht die erlaubten Aufrufe an Graphiti weiter. Sie implementiert Graphiti-Funktionen nicht erneut. Dann darf kein Agent den Rohendpunkt umgehen.

Für den Pilot verwenden wir **einen gemeinsamen `group_id`**, etwa `personal-global`. `personal`, `work`, `home`, `software`, Projektname, Quell-Channel und Vertraulichkeit sind Such-/Herkunftsattribute im gemeinsamen Memory, keine separaten Graphiti-Gruppen. So können zum Beispiel DLR und mehrere Projekte als dieselbe Entity verknüpft werden. Eine spätere echte Isolation ist nur für Bereiche mit eigener Berechtigungsgrenze vorgesehen und braucht eine ausdrückliche Designentscheidung. `group_id` allein ist keine Zugriffskontrolle.

### Voraussetzungen

- [x] Graphiti-Bestand auf `Codex-Pi`, `centralserver` und `nas1` geprüft: zuvor kein laufender Graphiti-Dienst gefunden. Der Pi hat etwa 3,2 GiB verfügbaren RAM und 16 GiB freien Speicher; seine `/tmp`-Ramdisk ist derzeit voll. `centralserver` hat etwa 21 GiB verfügbaren RAM und 1,6 TiB freien Cache-Speicher und betreibt bereits Ollama. `centralserver` ist als Betriebsort freigegeben und gewählt.
- [x] Für den Pilot FalkorDB im offiziellen kombinierten Graphiti-Image gewählt. Das Image ist per Digest gepinnt; Compose- und Unraid-Vorlage liegen in `ops/graphiti/`. Der Graph liegt persistent unter `/mnt/cache/appdata/graphiti-memory/falkordb/`. Backup-Abdeckung und Wiederherstellung sind noch zu prüfen. Neo4j bleibt eine Option bei anderen Betriebsanforderungen. Kuzu wegen der angekündigten Ablösung nicht für ein neues System wählen.
- [x] Graphiti-MCP-Server als `graphiti-memory` auf `centralserver` gestartet; `/health` antwortet lokal mit HTTP 200 und `/mcp` führt `initialize` und `tools/list` aus. Ein synthetischer MCP-Test hat Speichern, Faktensuche und Episodenlöschung bestätigt; danach enthielt der Graph null Knoten und null Kanten. Ein Schreib-/Neustart-/Lese-Test bestätigte die Persistenz des Datenverzeichnisses. Unraid-Vorlage: `/boot/config/plugins/dockerMan/templates-user/my-graphiti-memory.xml`. Der rohe Port `18000` ist ausschließlich an `127.0.0.1` gebunden. Vor Zugriff aus Mattermost/Codex oder durch andere Agenten die schlanke Kontrollschicht beziehungsweise einen gleichwertigen Berechtigungsweg bereitstellen. Konfiguration und Einmal-Smoke liegen in `ops/graphiti/`.
- [ ] Betrieb absichern: Unraids Docker-Image-Dateisystem liegt nach dem Pull bei etwa 89 % Belegung (11 GiB frei). FalkorDB warnt beim Start vor deaktiviertem `vm.overcommit_memory`; unter Speicherdruck kann ein Hintergrund-Save scheitern. Backup-Abdeckung für `/mnt/cache/appdata/graphiti-memory/falkordb/` und einen Restore noch prüfen, bevor echte Daten aufgenommen werden.
- [ ] LLM für Extraktion und Embedding-Modell fachlich qualifizieren. Technisch sind `qwen2.5:7b` und `nomic-embed-text` über den bestehenden Ollama-Endpunkt auf `centralserver` konfiguriert; Embeddings liefern 768 Dimensionen. Im Psono-Eintrag `Codex Secrets` ist derzeit kein OpenAI-API-Schlüssel aufgeführt. Der ChatGPT/Codex-Login der Runtime ist dafür nicht verwendbar. Die aktuelle Graphiti-MCP-Implementierung bietet entgegen dem README-Beispiel keinen `sentence_transformers`-Embedder; der OpenAI-kompatible Embedder nutzt Ollama. `json_object` lieferte in einem Test ein ungültiges Edge-Schema; `json_schema` verarbeitete eine synthetische Episode und lieferte einen suchbaren Fakt, formulierte ihn aber unerwartet auf Chinesisch. `qwen2.5:7b` unterschied zuvor eine Präferenz von einer bloßen Option, klassifizierte eine Korrektur aber fälschlich als nicht dauerhaft. `qwen2.5:3b` stufte die bloße Option als Entscheidung ein. Beide Modelle sind ohne weiteren Qualitätsnachweis nur für synthetische Pilotdaten freigegeben. Ingestion, Widersprüche, Sprache und Entity Resolution gegen einen Testdatensatz prüfen; gegebenenfalls stärkeren Provider wählen. Qualität, Kosten und mögliche Übertragung von Inhalten an den gewählten Provider prüfen. Secrets aus Psono beziehen.

### Datenmodell und Provenienz

- [ ] Mit Graphitis vorhandenen Entity-Typen `Person`, `Organization`, `Document`, `Event`, `Preference`, `Requirement`, `Procedure`, `Location`, `Topic` und `Object` beginnen. `Project`, `System` und `Decision` zunächst an Beispielen prüfen und nur ergänzen, wenn die generischen Typen nicht ausreichen. Einen kleinen Satz aussagekräftiger Beziehungstypen erst nach Sichtung realer Episoden definieren; keine große Ontologie vor dem Pilot festschreiben.
- [ ] Jede gespeicherte Aussage auf Episode und Ursprung zurückführen: Agent, Quellsystem, Channel/Projekt, Autor, Post-/Dokument-ID, Quellzeit, Speicherzeit, Vertrauens- und Zugriffsstatus. Bei Bedarf dafür einen separaten Herkunftsindex im Memory-Service führen; nicht annehmen, dass der Graphiti-MCP-Server alle Mattermost-Metadaten selbst verwaltet.
- [ ] Aussagen sauber unterscheiden: beschlossen, vorgeschlagen, möglich, veraltet, bestritten. Im Beispiel „DLR wäre eine Option“ darf `DLR ist Generalunternehmer` nicht als gesicherter Fakt entstehen. Widersprüchliche Quellen müssen sichtbar bleiben.
- [ ] Graphitis Zeitfelder (`valid_at`/`invalid_at`, Referenzzeit) für „was galt wann?“ nutzen. An Korrekturfällen prüfen, ob neue Episoden den alten Fakt tatsächlich invalidieren; falls nicht, muss der Memory-Service die Korrektur gezielt nachführen. Historie und Herkunft dürfen dabei nicht verloren gehen.

### Abruf, Speicherung und Zugriff

- [ ] Vor inhaltlichen Agentenantworten relevante Facts/Entities mit hybrider Suche abrufen, zeitlich und thematisch einordnen, Herkunft mitgeben und nur einen knappen, tokenbegrenzten Memory-Kontext einfügen. Die laufende Channel-Session bleibt für unmittelbare Gesprächsdetails zuständig. Relevanz und Latenz gegen einen Testdatensatz prüfen; für triviale Antworten kann der Service einen leeren Kontext zurückgeben.
- [ ] Nach einem Turn einen Memory-Controller entscheiden lassen: dauerhafte Präferenz, Projektfakt, Entscheidung oder wichtiges Ereignis speichern; Belangloses, Duplikate, bloße Vermutungen und unklare Aussagen auslassen oder als unsicher markieren. Nicht jeden User-/Bot-Post automatisch ingestieren. Episoden mit stabiler Quell-ID idempotent schreiben, auch nach Neustart oder Wiederholung.
- [ ] Globale Nutzbarkeit und Quellberechtigungen zusammenbringen: Der gemeinsame Graph darf Beziehungen aus allen Bereichen erkennen; bei Ausgabe in einem Channel dürfen nur Fakten erscheinen, deren Quellen der anfragende Nutzer und der Ziel-Channel sehen dürfen. Besonders private Channels, Arbeits-/Geheimhaltungsdaten und spätere externe Agenten prüfen. Für gemischte Herkunft braucht es eine Prüfung auf Fact-/Quellenebene. Die konkrete Freigaberegel für kanalübergreifende Fakten vor Implementierung festlegen.
- [ ] Korrektur, Löschung und Opt-out definieren: gelöschte oder berichtigte Posts und eine widerrufene Quellenfreigabe müssen im Memory wirksam werden. Nutzer müssen gespeicherte Fakten samt Herkunft einsehen, korrigieren und löschen können. Ein entfernter Bot verliert seinen Zugriff, ohne den gemeinsamen Graphen zu löschen.
- [ ] Fehlerverhalten bestimmen: Ein nicht erreichbares Graphiti darf den normalen Chat nicht blockieren; ein Ausfall oder fehlgeschlagener Memory-Schreibvorgang muss sichtbar und erneut bearbeitbar sein.

**Fertig, wenn:** Ein zulässiger Fakt nach Neustart der Session auch in einem anderen berechtigten Channel oder Agenten wiedergefunden wird, seine Quelle überprüfbar ist, eine Korrektur den früheren Stand historisch erhält und den aktuellen Stand richtig wiedergibt, und ein nicht berechtigter Nutzer keine geschützte Quelle oder daraus abgeleitete Aussage erhält.

## 3. Funktions- und Betriebstests

- [ ] Neuen Channel-Modus über mehrere Turns, mehrere Nutzer, parallele Nachrichten und Bot-Entfernung testen; Antworten müssen genau einmal und direkt im Channel erscheinen.
- [ ] Neustart von Plugin, Mattermost, Codex-Runtime und Memory-Service testen: Session-Fortsetzung, laufende Freigaben, Aufgaben/Erinnerungen und Graphiti-Memory prüfen.
- [ ] Memory-Tests mit konkreten Fällen durchführen: dauerhafte Präferenz, mögliche statt beschlossene Entscheidung, spätere Korrektur, widersprüchliche Quellen, zeitliche Frage, Wiederholung eines Events, erlaubte kanalübergreifende Erinnerung und gesperrte Herkunft.
- [ ] Bereits früher einzeln getestete Hermes-Funktionen im Zielablauf erneut prüfen: Dateien/Workspace, Erinnerungen, Freigabe und Fortsetzung, Supervisor/Subagenten, Voice, lokale Modellroute sowie Nutzungsgrenzen.
- [ ] Kosten und Last messen: Channel-Kontext, Graphiti-Extraktion, Embeddings und Suche getrennt erfassen; sinnvolle Grenzen und Aufbewahrungsdauer festlegen.
- [ ] Build, Konfiguration, Datenablage, Backup und Rückweg versioniert dokumentieren. Den stark veränderten Arbeitsbaum in reviewbare Änderungen überführen.

**Fertig, wenn:** Die vereinbarten Alltagsszenarien nach einem Neustart funktionieren und Restore sowie Rückweg praktisch geprüft sind.

## 4. Hermes-Off-Cutover

- [x] Grund für die ausgebliebenen täglichen Soak-Erfolge ermittelt: Die Prüfung läuft auf dem alten Host `centralserver`, wo `docker exec mattermost` und der lokale Voice-Adapter nicht mehr funktionieren. Der bloße Ablauf von sieben Tagen erfüllt die Bedingung nicht: Das Skript erwartet sieben verschiedene erfolgreiche Testtage; derzeit sind es zwei.
- [ ] Daily Smoke und Preflight auf die aktuelle Topologie umstellen: Mattermost/Codex auf `nas1`, Ollama auf `centralserver`, Voice-Adapter am tatsächlichen Ort. Bot-ID, Bot-Name, Test-Channel-Namen und n8n-Prüfungen aktualisieren; `unknown` als eigene Diagnose ausgeben und nicht als bestätigte aktive Hermes-Nutzung interpretieren.
- [ ] Repräsentative Test-Channels mindestens sieben erfolgreiche Tage überwachen. Danach Checkliste und Produktions-Channel-Regeln mit dem tatsächlich ausgerollten Build prüfen.
- [ ] Hermes-Adapter erst nach grünem Preflight und geprüftem Rückweg deaktivieren; anschließend Produktiv-Channel und Hintergrundaufgaben beobachten.

**Fertig, wenn:** Preflight und Plugin-Checkliste `ready=true` melden, die Produktivfälle bestehen und Hermes ohne Verlust benötigter Funktionen abgeschaltet ist.

## Reihenfolge und offene Entscheidungen

1. MCP-Inventur und Runtime-Anbindung; parallel vorhandene Graphiti-Instanz und Betriebsort klären.
2. Gemeinsames Memory-Design (Datenaufnahme, Herkunft, Rechte, Zeitmodell) festziehen und Graphiti-Pilot mit zwei berechtigten Test-Channels sowie einem gesperrten Zugriff durchführen.
3. Funktions- und Neustarttests, danach sieben erfolgreiche Soak-Tage.
4. Abschaltentscheidung und Cutover.

Vor dem Graphiti-Pilot sind konkret zu entscheiden: **Betriebsort**, **FalkorDB oder vorhandenes Neo4j**, **LLM- und Embedding-Provider**, **welche Inhalte dauerhaft gespeichert werden dürfen** und **welche Nutzer/Agenten Fakten aus welchen Quellen abrufen dürfen**. Die gemeinsame Memory-Struktur und der eine logische Graph sind gesetzt; die Freigaberegeln für Herkunft und Ausgabe sind noch offen.

## Quellen

- Projektcode: `conversations/channel_conversations.go`, `server/codex_runtime.go`, `ops/hermes-off-soak/`.
- Betriebsstand: `/root/Documents/Codex/2026-08-18/ich-habe-eine-fork-von-mattermost/hermes-off-auto-finalize.log` und `hermes-off-cutover-runbook.md` im selben Verzeichnis.
- Graphiti-Projekt: https://github.com/getzep/graphiti
- Graphiti-MCP-Server: https://github.com/getzep/graphiti/blob/main/mcp_server/README.md
- Graphiti-MCP-Konfiguration: https://github.com/getzep/graphiti/blob/main/mcp_server/config/config.yaml
