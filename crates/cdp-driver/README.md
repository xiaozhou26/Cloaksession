# cdp-driver
Wraps chromiumoxide: safe CDP enable-refcount (rejects Runtime/Network enables on CloakBrowser to avoid DCHECK), bootstrap target emulation (WebRTC/preload/locale/UA-CH, engine-gated), and the 8 browser-drive tools (navigate/click/type/extract/screenshot/evaluate + behavioral injection). Connects by fetching `webSocketDebuggerUrl` from `/json/version` then `Browser::connect(ws)`.
