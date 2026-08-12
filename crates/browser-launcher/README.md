# browser-launcher
Spawns CloakBrowser/CFT, passes `--fingerprint-*` / `--proxy-server` / `--user-data-dir` / `--load-extension` flags, runs the local SOCKS5 bridge (remote DNS), probes proxy geo via ipapi.co, manages session-restore prefs and singleton locks, and tracks running profiles in a registry. Does NOT issue CDP commands — that's `cdp-driver`.
