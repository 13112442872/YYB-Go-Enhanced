# Magisk module (experimental)

This package runs YYB Go directly from Magisk's late-start service stage. It
does not require Termux to remain open.

## Current scope

- Android arm64 only.
- Magisk 20.4 or newer.
- The console listens on `127.0.0.1:8000` by default.
- Protocol accounts, logs, configuration, avatars, and QR files persist under
  `/data/adb/yyb-go` across module upgrades.
- Browser authentication remains optional. It requires a reachable external
  MySQL server when enabled.

The module has been cross-compiled and structurally validated, but it still
needs installation testing on a rooted Android device before it should be
published as a stable release.

## Build

The build host needs Go 1.23+, Bash, and `zip`.

```sh
VERSION=0.1.0 VERSION_CODE=1 ./scripts/build-magisk.sh arm64
```

The ZIP is written to `dist/` and can be installed from the Magisk app.

## Runtime

After reboot, open the Magisk app and press the module's Action button. It
opens `http://127.0.0.1:8000/` in the default browser.

Configuration is created on first start:

```text
/data/adb/yyb-go/config.conf
```

Edit that file as root, then stop and start the module or reboot. Runtime files
are stored here:

```text
/data/adb/yyb-go/resource/db
/data/adb/yyb-go/resource/avatars
/data/adb/yyb-go/resource/qr
/data/adb/yyb-go/yyb-go.log
```

## Network and authentication

Keep `HOST=127.0.0.1` for normal phone-only use. Android loopback is shared by
apps on the same device, so this prevents LAN access but is not an app-level
security boundary. Setting `HOST=0.0.0.0` also exposes the console and protocol
API to the local network. Do not do that without firewall rules or browser
authentication.

Browser login is enabled only when `YYB_AUTH_MYSQL_DSN` points to a reachable
MySQL instance. With no MySQL DSN, the local console opens without a login page.

If WeChat endpoints do not resolve on a specific Android ROM, collect
`/data/adb/yyb-go/yyb-go.log`, the Android version, ROM name, and Magisk version.
Android DNS behavior differs between vendors and needs device-level testing.

## Uninstall behavior

Uninstalling stops the service but intentionally preserves `/data/adb/yyb-go`.
Delete that directory manually only after confirming the stored accounts are no
longer needed.
