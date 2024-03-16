# Forschungsarbeitbörse

Die Forschungsarbeitbörse ist eine **Open Source Webanwendung zur Veröffentlichung
von Angeboten für Forschungs- und Doktorarbeiten**. Mit einem **Fokus auf einfache
Zugänglichkeit und Administration** ohne Registrierung und dabei minimalen
Installationsanforderungen.

## Installation

Installationsbeispiel Linux:

<details>

```
# Download oder Build des Binary

sudo install -m 0755 bin/forschungsarbeitboerse /usr/local/bin/

sudo adduser --system --disabled-password --home /var/lib/forschungsarbeitboerse --shell /bin/bash --gecos '' --group forschungsarbeitboerse

sudo install -m 0640 forschungsarbeitboerse.toml.example -o forschungsarbeitboerse -g forschungsarbeitboerse /var/lib/forschungsarbeitboerse/forschungsarbeitboerse.toml

# Anpassung der Beispielkonfiguration

# Konfiguration eines systemd Unit (siehe unten)
# Konfiguration eines Reverse Proxy (siehe unten)
```

</details>

Initialisierung der SQLite Datenbank:

```
sudo su - forschungsarbeitboerse
forschungsarbeitboerse -init-db
```

[systemd](https://systemd.io/) Beispielkonfiguration und -installation:

<details>

```
cat <<SERVICE | sudo tee /etc/systemd/system/forschungsarbeitboerse.service
[Unit]
Description=forschungsarbeitboerse
After=network-online.target
Wants=network-online.target systemd-networkd-wait-online.service

[Service]
Restart=on-failure

User=forschungsarbeitboerse
Group=forschungsarbeitboerse

WorkingDirectory=/var/lib/forschungsarbeitboerse

ExecStart=/usr/local/bin/forschungsarbeitboerse

NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=full

[Install]
WantedBy=multi-user.target
SERVICE

sudo systemctl daemon-reload
sudo systemctl enable forschungsarbeitboerse
sudo systemctl start forschungsarbeitboerse
```

</details>

[Caddy](https://caddyserver.com) Reverse Proxy Beispielkonfiguration:

<details>

```
forschungsarbeitboerse.example.com {
	encode gzip

	log {
		format json
		output file /var/log/caddy/forschungsarbeitboerse.example.com.access.json
	}

	reverse_proxy /* 127.0.0.1:4444
}
```

</details>

## Nutzung

```
forschungsarbeitboerse -config forschungsarbeitboerse.toml
```

## Konfiguration

Die Konfiguration erfolgt über eine einfache Textdatei `forschungsarbeitboerse.toml`,
ein Template zum Download findet sich hier:
[`forschungsarbeitboerse.toml.example`](https://raw.githubusercontent.com/forschungsarbeitboerse/forschungsarbeitboerse/master/forschungsarbeitboerse.toml.example)

Beispielkonfiguration mit allen Parametern:

<details>

```
# Die Root URL ihrer Installation, bspw. "https://forschungsarbeitboerse.example.com"
url = "http://127.0.0.1:4444"

# Socket IP Adresse und Port
addr = "127.0.0.1:4444"

# Ein individuelles 32 Byte Cookie Secret in hexadezimaler Notation;
# `forschungsarbeitboerse -gen-cookie-secret`
cookie_secret = ""

# SMTP Zugangsdaten zur Versendung der administrativen E-Mails
smtp_user = "example@example.com"
smtp_pass = "SECRET"
smtp_host = "mail.example.com"
smtp_port = "587"
smtp_mail_from = "noreply@example.com"

# E-Mail Adressen (RegExp, https://pkg.go.dev/regexp/syntax), für die
# Nutzer:innen das Angebot selbst freischalten dürfen; alle anderen
# Angebote erfordern eine Freischaltung durch die Administratoren
valid_mail_regexp = [".+@example.com"]

# E-Mail Adresse der Administratoren
admin_email = "admins@example.com"

# Auswahl Art der Arbeit
posting_categories = [
	"Doktorarbeit",
	"Forschungsarbeit",
]

# Auswahl Typ der Arbeit
posting_types = [
	"experimentell",
	"tierexperimentell",
	"klinisch",
	"statistisch",
	"theoretisch",
]

# Im Menü angezeigter Titeltext
title_text = "Forschungsarbeitbörse"

# Auf der Startseite angezeigter Infotext (Markdown)
info_text = """
Auf der **Forschungs- und Doktorarbeitbörse** können Sie unkompliziert
und ohne Registrierung Angebote für wissenschaftliche Arbeiten finden
und einstellen. Die Freischaltung und Administration erfolgt über E-Mail.
"""

# Der Footer Text (Markdown)
footer_text = """
Kontakt & Hilfe: <forschungsarbeitboerse@example.com>
"""
```

</details>

## Lizenz

Die Forschungsarbeitbörse ist unter den Bedingungen der Open Source Lizenz
AGPL v3 frei und kostenlos nutzbar:

[GNU Affero General Public License v3](https://www.gnu.org/licenses/agpl-3.0.en.html)

## Development

### Requirements

* gcc
* git
* Go `>= 1.21`
* Make

```
git clone https://github.com/forschungsarbeitboerse/forschungsarbeitboerse
cd forschungsarbeitboerse
```

### Build

```
make
```

### Live reload

Mit [air](https://github.com/cosmtrek/air) kann die Anwendung live neu kompiliert
und geladen werden:

```
make dev
```
