use chromiumoxide::Page;
use chromiumoxide::cdp::browser_protocol::target::TargetId;
use chromiumoxide::handler::HandlerConfig;
use chromiumoxide::{Command, Method};
use multizen_core::{BrowserEngine, MultizenError, Result};
use serde::ser::Serializer;
use tokio::sync::Mutex;

use crate::safe_cdp::{self, SafeEnableRefcount};

#[derive(Debug)]
struct RawCdpCommand {
    method: String,
    params: serde_json::Value,
}

impl serde::Serialize for RawCdpCommand {
    fn serialize<S>(&self, serializer: S) -> std::result::Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        self.params.serialize(serializer)
    }
}

impl Method for RawCdpCommand {
    fn identifier(&self) -> chromiumoxide::types::MethodId {
        self.method.clone().into()
    }
}

impl Command for RawCdpCommand {
    type Response = serde_json::Value;
}

/// Retry a GET request that returns JSON, sleeping `delay_ms` between
/// attempts, up to `max_attempts` times. Used to wait for Chromium's CDP
/// port to become ready after process spawn.
async fn retry_get_json(
    url: &str,
    max_attempts: usize,
    delay_ms: u64,
) -> std::result::Result<serde_json::Value, String> {
    let mut last_err = String::new();
    for i in 0..max_attempts {
        match reqwest::get(url).await {
            Ok(resp) => match resp.json::<serde_json::Value>().await {
                Ok(v) => return Ok(v),
                Err(e) => last_err = format!("version json: {e}"),
            },
            Err(e) => last_err = e.to_string(),
        }
        if i + 1 < max_attempts {
            tokio::time::sleep(std::time::Duration::from_millis(delay_ms)).await;
        }
    }
    Err(last_err)
}

pub struct BrowserSession {
    pub browser: chromiumoxide::Browser,
    pub engine: BrowserEngine,
    pub safe: SafeEnableRefcount,
    /// Active page used by all tools (navigate/screenshot/evaluate/click/
    /// type_text/extract). `None` until the first navigate or first tool
    /// call. `Page` is `Clone` and stores shared browser state, so we keep
    /// an `Option<Page>` and clone on retrieval.
    active_page: Mutex<Option<Page>>,
}

impl BrowserSession {
    pub async fn connect(cdp_endpoint: &str, engine: BrowserEngine) -> Result<Self> {
        // cdp_endpoint is http://127.0.0.1:{port}. Fetch webSocketDebuggerUrl.
        // Retry for up to ~10s because Chromium takes a moment to open the CDP
        // port after the process is spawned; an immediate fetch fails with
        // connection refused.
        let version_url = format!("{cdp_endpoint}/json/version");
        let resp = retry_get_json(&version_url, 20, 500)
            .await
            .map_err(|e| MultizenError::Cdp(format!("version fetch: {e}")))?;
        let ws_url = resp
            .get("webSocketDebuggerUrl")
            .and_then(|v| v.as_str())
            .ok_or_else(|| MultizenError::Cdp("no webSocketDebuggerUrl".into()))?
            .to_string();

        let handler_config = HandlerConfig {
            ignore_invalid_messages: true,
            ..HandlerConfig::default()
        };
        let (browser, mut handler) = chromiumoxide::Browser::connect_with_config(&ws_url, handler_config)
            .await
            .map_err(|e| MultizenError::Cdp(format!("connect: {e}")))?;
        // Drive the CDP handler in background. `Handler` implements
        // `futures::Stream`; poll it forever so the CDP connection stays alive.
        tokio::spawn(async move {
            use futures::StreamExt;
            while let Some(_h) = handler.next().await {}
        });

        let session = Self {
            browser,
            engine,
            safe: SafeEnableRefcount::new(),
            active_page: Mutex::new(None),
        };

        // C1: The safe-CDP gate is not yet wired into chromiumoxide's
        // auto-enable. chromiumoxide auto-enables `Runtime` and `Page` on
        // `Browser::connect`/`new_page`; on CloakBrowser, `Runtime` is
        // exactly the `CLOAK_RISKY_ENABLE_DOMAINS` tripwire. Log the gap so
        // integrators (Plan 3) see it; a full enforcement gate is deferred.
        if engine == BrowserEngine::Cloakbrowser {
            tracing::warn!(
                engine = "cloakbrowser",
                "safe_cdp gate not yet wired into chromiumoxide auto-enable; \
                 CloakBrowser sessions may trip the Runtime/Network DCHECK and crash. \
                 See safe_cdp module comment for the deferred enforcement plan."
            );
        }

        Ok(session)
    }

    /// Returns the active `Page`, creating one if none is set yet. `Page` is
    /// `Clone` in chromiumoxide 0.7 (it wraps `Arc<PageInner>`), so we clone
    /// on retrieval — callers get an owned `Page` that is cheap to hold
    /// across await points.
    pub(crate) async fn active_page(&self) -> Result<Page> {
        let mut guard = self.active_page.lock().await;
        if let Some(ref p) = *guard {
            return Ok(p.clone());
        }
        // Fall back to the first existing page, if any.
        let page = self
            .browser
            .pages()
            .await
            .map_err(|e| MultizenError::Cdp(format!("pages: {e}")))?
            .into_iter()
            .next()
            .ok_or_else(|| MultizenError::Cdp("no page".into()))?;
        *guard = Some(page.clone());
        Ok(page)
    }

    /// Set/replace the active page (used by `navigate` after it creates or
    /// reuses a page).
    pub(crate) async fn set_active_page(&self, page: Page) {
        let mut guard = self.active_page.lock().await;
        *guard = Some(page);
    }

    /// Select an attached page by target id for subsequent page operations.
    pub async fn activate_page(&self, target_id: &str) -> Result<()> {
        let activate = RawCdpCommand {
            method: "Target.activateTarget".into(),
            params: serde_json::json!({ "targetId": target_id }),
        };
        self.browser
            .execute(activate)
            .await
            .map_err(|e| MultizenError::Cdp(format!("activate page {target_id}: {e}")))?;
        let page = self
            .browser
            .get_page(TargetId::new(target_id))
            .await
            .map_err(|e| MultizenError::Cdp(format!("get page {target_id}: {e}")))?;
        self.set_active_page(page).await;
        Ok(())
    }

    /// Create and attach a new page, making it the active page for subsequent operations.
    pub async fn new_page(&self, url: &str) -> Result<String> {
        let page = self
            .browser
            .new_page(url)
            .await
            .map_err(|e| MultizenError::Cdp(format!("new page {url}: {e}")))?;
        let target_id = page.target_id().as_ref().to_string();
        self.set_active_page(page).await;
        Ok(target_id)
    }

    /// Close a page and select another attached page if the active page was closed.
    pub async fn close_page(&self, target_id: &str) -> Result<()> {
        let page = self
            .browser
            .get_page(TargetId::new(target_id))
            .await
            .map_err(|e| MultizenError::Cdp(format!("get page {target_id}: {e}")))?;
        let was_active = self
            .active_page
            .lock()
            .await
            .as_ref()
            .is_some_and(|active| active.target_id().as_ref() == target_id);
        page.close()
            .await
            .map_err(|e| MultizenError::Cdp(format!("close page {target_id}: {e}")))?;
        if was_active {
            let next = self
                .browser
                .pages()
                .await
                .map_err(|e| MultizenError::Cdp(format!("pages after close: {e}")))?
                .into_iter()
                .find(|candidate| candidate.target_id().as_ref() != target_id);
            let mut guard = self.active_page.lock().await;
            *guard = next;
        }
        Ok(())
    }

    /// Execute an allow-listed raw CDP command against the active page.
    pub async fn cdp_send(
        &self,
        method: &str,
        params: Option<serde_json::Value>,
        session_id: Option<&str>,
    ) -> Result<serde_json::Value> {
        let page = self.active_page().await?;
        let target_session = page.session_id().as_ref();
        if session_id.is_some_and(|id| id != target_session) {
            return Err(MultizenError::Cdp(
                "explicit CDP session_id does not match the active page".into(),
            ));
        }
        let command = |method: &str, params: Option<serde_json::Value>| RawCdpCommand {
            method: method.to_string(),
            params: params.unwrap_or_default(),
        };
        let response = match method {
            "Target.getTargets" | "Target.createTarget" | "Target.activateTarget" | "Target.closeTarget" => self
                .browser
                .execute(command(method, params))
                .await
                .map_err(|e| MultizenError::Cdp(format!("{method}: {e}")))?,
            _ => page
                .execute(command(method, params))
                .await
                .map_err(|e| MultizenError::Cdp(format!("{method}: {e}")))?,
        };
        Ok(response.result)
    }

    /// `data-mz-add-ext` DOM attribute. If found, the attribute is cleared
    /// and its value is returned. This is the companion channel: the content
    /// script on Chrome Web Store pages writes the extension id to
    /// `<html data-mz-add-ext="…">` and the host polls it via
    /// `Runtime.evaluate`. The DOM is shared across content-script worlds,
    /// so this works even though CloakBrowser isolates content scripts.
    ///
    /// Returns the first non-empty attribute value found across all matching
    /// pages, or `None` if no page has a signal.
    pub async fn poll_companion_signal(&self, url_filter: &str) -> Result<Option<String>> {
        let pages = self
            .browser
            .pages()
            .await
            .map_err(|e| MultizenError::Cdp(format!("pages: {e}")))?;

        for page in pages {
            // Check the URL — only poll Chrome Web Store pages.
            let url = match page.evaluate("location.href").await {
                Ok(eval) => eval
                    .into_value::<serde_json::Value>()
                    .ok()
                    .and_then(|v| v.as_str().map(|s| s.to_string()))
                    .unwrap_or_default(),
                Err(_) => continue, // page might be navigating — skip
            };
            if !url.contains(url_filter) {
                continue;
            }
            // Read + clear the attribute atomically.
            let expr = "(function(){var e=document.documentElement,v=e.getAttribute('data-mz-add-ext');if(v)e.removeAttribute('data-mz-add-ext');return v;})()";
            match page.evaluate(expr).await {
                Ok(eval) => {
                    if let Ok(v) = eval.into_value::<serde_json::Value>() {
                        if let Some(s) = v.as_str() {
                            if !s.is_empty() {
                                return Ok(Some(s.to_string()));
                            }
                        }
                    }
                }
                Err(_) => {
                    // Page might be navigating — skip.
                }
            }
        }
        Ok(None)
    }

    /// safe-CDP gate check for a domain. Returns `true` if the domain is not
    /// yet enabled (`SafeEnableRefcount::should_enable`) AND CloakBrowser
    /// policy allows it (`cloak_allows_domain`). Plan 3 should call this
    /// before issuing domain-enabling CDP commands. Today tools use it for
    /// `tracing::debug!` observability only (chromiumoxide already
    /// auto-enabled `Runtime`), so `SafeEnableRefcount` and
    /// `cloak_allows_domain` are not dead code.
    pub fn safe_enable_check(&self, domain: &str) -> bool {
        self.safe.should_enable(domain) && safe_cdp::cloak_allows_domain(domain, self.engine)
    }
}
