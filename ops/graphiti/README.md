# Graphiti-Pilot

Die Compose-Datei und die lokale Unraid-Vorlage nutzen das offizielle kombinierte Graphiti/FalkorDB-Image. Sie sind für einen **lokalen Pilot** vorbereitet. Der rohe MCP-Port wird nur an `127.0.0.1` gebunden; Graphiti selbst prüft weder Mattermost-Channel-Rechte noch die Herkunft einer Anfrage. Vor Zugriff durch mehrere Agenten kommt der geplante Memory-Service davor.

## Unraid auf `centralserver`

Die Vorlage `unraid-template.xml` wird auf dem Server als `/boot/config/plugins/dockerMan/templates-user/my-graphiti-memory.xml` abgelegt. Die Konfiguration liegt unter `/mnt/cache/appdata/graphiti-memory/config.yaml`, der persistente Graph unter `/mnt/cache/appdata/graphiti-memory/falkordb/`. Das offizielle Kombi-Image wird per Digest gepinnt. Das Feld „Extra Parameters“ der Vorlage bindet nur `127.0.0.1:18000` an den MCP-Port 8000; weder FalkorDB noch dessen Browser bekommen einen Host-Port. Beim Bearbeiten in Unraid muss diese Bindung erhalten bleiben.

Der lokale Dienst antwortet unter `http://127.0.0.1:18000/health` und `http://127.0.0.1:18000/mcp`. Beide Adressen funktionieren nur auf `centralserver`. Vor echten Daten den Graph-Pfad in eine Backup-Strategie aufnehmen und eine Wiederherstellung prüfen. Ein gesicherter Zugriff für Mattermost, Codex und weitere Agenten gehört erst hinter den geplanten Memory-Service.

Stand 22.09.2026: Der Container ist in Unraid angelegt und läuft mit dem offiziellen Image. Der MCP-Test hat eine synthetische Episode gespeichert, einen Fakt gesucht und die Episode gelöscht. Danach waren null Knoten und null Kanten vorhanden. Ein Schreib-/Neustart-/Lese-Test hat die Persistenz des eingebundenen FalkorDB-Verzeichnisses bestätigt. Das lokale `qwen2.5:7b` formulierte einen englischen Testfakt allerdings auf Chinesisch; reale Inhalte bleiben bis zur Qualitätsprüfung ausgesetzt. Der FalkorDB-Start meldet zudem `vm.overcommit_memory` als deaktiviert; bei knappem RAM kann das Hintergrundspeichern beeinträchtigen.

## Voraussetzungen

1. Host und persistentes Datenverzeichnis festlegen. `GRAPHITI_DATA_DIR` muss ein absoluter Pfad auf einem gesicherten Laufwerk sein. Datenverzeichnis in die Backup-Strategie aufnehmen und einen Restore prüfen.
2. Auf Unraid ist Docker bereits vorhanden. Für andere Hosts ist Docker Compose nötig. Auf `Codex-Pi` wäre dafür die DietPi-Software-ID `162` (Docker) und `134` (Docker Compose) vorgesehen.
3. Ein strukturiert ausgebendes Extraktionsmodell und ein Embedding-Modell über einen OpenAI-kompatiblen Endpunkt bereitstellen. Die Voreinstellungen nutzen den bestehenden Ollama-Endpunkt auf `centralserver` (`192.168.1.16:11434`). `nomic-embed-text` wurde dort installiert; die OpenAI-kompatible API liefert 768 Dimensionen. `qwen2.5:7b` wurde ergänzt und erkannte im ersten Test eine Präferenz und eine bloße Option korrekt, verwarf aber im nächsten Test eine wichtige Projektkorrektur als transient. Das zuvor vorhandene `qwen2.5:3b` stufte die Option fälschlich als Entscheidung ein. Deshalb ausschließlich synthetische Pilotdaten verwenden, bis die Qualitätsprüfung abgeschlossen ist.

## Start und Prüfung

Beispiel mit einer zur Laufzeit gesetzten Datenablage:

```sh
GRAPHITI_DATA_DIR=/pfad/zur/persistenten/graphiti-ablage docker compose -f ops/graphiti/compose.yaml up -d
curl -fsS http://127.0.0.1:18000/health
```

Die URL `http://127.0.0.1:18000/mcp` ist nur vom Host erreichbar. `python3 ops/graphiti/smoke.py` fügt einen unkritischen Beispielsatz hinzu, wartet auf einen suchbaren Fact und löscht die Testepisode. Danach einen Neustart und eine fachliche Korrektur separat prüfen. Ein erfolgreicher Redis-Healthcheck allein belegt noch keine funktionierende Graphiti-Ingestion.

Die Umgebung kann `GRAPHITI_PROVIDER_URL`, `GRAPHITI_PROVIDER_KEY`, `GRAPHITI_LLM_MODEL`, `GRAPHITI_EMBEDDER_MODEL`, `GRAPHITI_EMBEDDING_DIM` und `GRAPHITI_LOCAL_PORT` überschreiben. Keine echten Schlüssel in Compose-Dateien oder Shell-History schreiben; Secrets aus Psono verwenden. Der ChatGPT/Codex-Login ist kein API-Schlüssel für Graphiti.

## Quellen und Stand

- Upstream-Compose und MCP-Dokumentation: https://github.com/getzep/graphiti/blob/main/mcp_server/docker/docker-compose.yml und https://github.com/getzep/graphiti/blob/main/mcp_server/README.md
- Der aktuelle MCP-Server implementiert den in der README erwähnten `sentence_transformers`-Embedder in seiner Provider-Auswahl nicht. Daher nutzt dieses Setup den vorhandenen OpenAI-kompatiblen Embedder-Pfad zu Ollama: https://github.com/getzep/graphiti/blob/main/mcp_server/src/services/factories.py
