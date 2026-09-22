# Stand und Rückbau des Mattermost-Codex-Plugins

Stand: 22.09.2026. Dieses Dokument hält den erreichten Entwicklungsstand und die Entscheidung fest, **keinen Custom Plugin mehr in Mattermost zu betreiben**. Der Quellstand auf dem Feature-Branch dient als Archiv und Ausgangspunkt für eine mögliche spätere Lösung außerhalb eines eigenen Mattermost-Plugins. Der ältere [Hermes-Ersatz-Plan](hermes_replacement_plan.md) beschreibt die zuvor verfolgte Richtung; seine Plugin- und Cutover-Aufgaben sind durch diese Entscheidung ausgesetzt.

## Was umgesetzt und beobachtet wurde

- Der Fork des Mattermost Agents Plugins wurde um eine Codex-App-Server-Runtime ergänzt. Sie nutzt den Codex-Login und einen eigenen `CODEX_HOME` im Mattermost-Container; sie ist kein direkter OpenAI-API-Client. Die MCP-Konfiguration der Pi-Instanz wird nicht automatisch übernommen.
- Ein Bot konnte als Mitglied eines Channels auf jede menschliche Nachricht reagieren, auch ohne `@`-Erwähnung. Seine Antworten erschienen als normale Posts im Channel. Pro Channel und Bot wurde eine gemeinsame, persistente Codex-Session genutzt. Beim ersten Turn wurden höchstens 30 frühere Posts beziehungsweise 12.000 Zeichen als Startkontext übergeben. Der Test in `Codex Hermes-Off Cloud Test` bestätigte Reaktion ohne Erwähnung, sichtbaren Channel-Post und Verwendung einer früheren Channel-Nachricht. Das war ein Funktionstest, kein vollständiger Produktionsnachweis.
- Weitere Entwicklungsarbeit im nicht veröffentlichten Arbeitsbaum umfasst Runtime-Steuerung und Richtlinien, Sitzungen, Freigaben, Aufgaben/Erinnerungen, Supervisor-Abläufe, Workspace-Dateien, Slash-Commands, lokale Modellroute, Voice/TTS und Administrationsoberflächen. Der Stand wurde nicht als fertiger Hermes-Ersatz abgenommen. Quellcode, Tests und Migrationen sind im Feature-Branch archiviert; generierte Build-Verzeichnisse bleiben lokal und gehören nicht zur Quelle.
- Die frühere Hermes-Off-Automatik meldete `ready=false`. Ihre Infrastrukturprüfung lief noch auf `centralserver`, obwohl Mattermost inzwischen auf `nas1` liegt. Zwei erfolgreiche Soak-Tage waren registriert; sieben verschiedene erfolgreiche Tage waren vorgesehen. Die veraltete Topologie und `unknown`-Werte aus n8n verhinderten eine belastbare Cutover-Aussage. Hermes wurde durch diesen Entwicklungsstand nicht ersetzt.
- Für ein gemeinsames Langzeitgedächtnis wurde ein Graphiti/FalkorDB-Pilot mit dem offiziellen kombinierten Docker-Image als Unraid-Vorlage auf `centralserver` eingerichtet. Der MCP-Port ist nur an `127.0.0.1:18000` gebunden. Ein synthetischer Test bestätigte Speichern, Suchen, Löschen und Datenpersistenz nach Neustart. Es wurden keine echten Mattermost-Channel-Daten in Graphiti aufgenommen und keine Mattermost-Anbindung hergestellt. Details stehen in [`ops/graphiti/README.md`](../ops/graphiti/README.md).

## Was noch offen war

- Die getrennte Codex-Runtime hätte eine gezielte MCP-Konfiguration und Berechtigungsprüfung für jedes Werkzeug gebraucht. Besonders Psono, Produktions-n8n und Host-Administration durften nicht pauschal an Channel-Bots gehen.
- Vor echtem gemeinsamem Memory wären ein agentenunabhängiger Memory-Controller, Herkunft und Quellrechte, selektive Speicherung, Korrektur und Löschung, Backup/Restore sowie ein Qualitätstest des Extraktionsmodells erforderlich gewesen. `qwen2.5:7b` erzeugte im Pilot einmal ein ungültiges Edge-Schema und formulierte einen englischen Testfakt auf Chinesisch. Das lokale Modell ist damit noch nicht für echte Inhalte qualifiziert.
- Der Hermes-Ersatz hätte weitere Funktions-, Neustart- und Soak-Tests sowie einen aktualisierten Cutover-Plan benötigt. Diese Arbeit wird für den Custom Plugin Ansatz nicht fortgesetzt.

## Entscheidung und Rückbaugrenze

Der Nutzer möchte keinen Custom Plugin mehr in seiner Mattermost-Instanz. Nach Sicherung dieses Quellstands wird die installierte Fork-Version des Plugins `mattermost-ai` auf der laufenden Instanz deinstalliert. Plugin-spezifische Bots und Registrierungen werden geprüft und deaktiviert, soweit sie zu diesem Versuch gehören. Andere Mattermost-Plugins und der Mattermost-Server bleiben Gegenstand ihrer eigenen Verwaltung. Historische Channel-Posts werden nicht als Voraussetzung für die Deinstallation gelöscht.

Die Graphiti-Instanz auf `centralserver` ist ein getrennter Pilotdienst ohne Mattermost-Anbindung. Ihre weitere Verwendung oder ihr Rückbau ist eine eigene Infrastrukturentscheidung. Der hier dokumentierte Rückbau betrifft die Mattermost-Installation.

## Nachweis nach dem Rückbau

Nach der Deinstallation sind Plugin-Liste, Plugin-Dateien, zugehörige Bot-Aktivität und der Wegfall des Plugin-Endpunkts zu prüfen. Der tatsächliche Zeitpunkt und die Ergebnisse werden hier nachgetragen.
