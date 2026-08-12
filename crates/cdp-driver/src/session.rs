use chromiumoxide::Page;
use multizen_core::{BrowserEngine, MultizenError, Result};
use tokio::sync::Mutex;

use crate::safe_cdp::{self, SafeEnableRefcount};

pub struct BrowserSession {
    pub browser: chromiumoxide::Browser,
    pub engine: BrowserEngine,
    pub safe: SafeEnableRefcount,
    /// Active page used by all tools (navigate/screenshot/evaluate/click/
    /// type_text/extract). `None` until the first navigate or first tool
    /// call. `Page` is `Clone` (wraps `Arc<PageInner>` in chromiumoxide 0.7)
    /// so we store an `Option<Page>` and clone on retrieval.
    active_page: Mutex<Option<Page>>,
}

impl BrowserSession {
    pub async fn connect(cdp_endpoint: &str, engine: BrowserEngine) -> Result<Self> {
        // cdp_endpoint is http://127.0.0.1:{port}. Fetch webSocketDebuggerUrl.
        let version_url = format!("{cdp_endpoint}/json/version");
        let resp = reqwest::get(&version_url)
            .await
            .map_err(|e| MultizenError::Cdp(format!("version fetch: {e}")))?
            .json::<serde_json::Value>()
            .await
            .map_err(|e| MultizenError::Cdp(format!("version json: {e}")))?;
        let ws_url = resp
            .get("webSocketDebuggerUrl")
            .and_then(|v| v.as_str())
            .ok_or_else(|| MultizenError::Cdp("no webSocketDebuggerUrl".into()))?
            .to_string();

        let (browser, mut handler) = chromiumoxide::Browser::connect(&ws_url)
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
