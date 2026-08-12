use std::time::Duration;

use chromiumoxide::cdp::browser_protocol::page::CaptureScreenshotFormat;
use chromiumoxide::page::ScreenshotParams;
use multizen_core::{MultizenError, Result};

pub struct NavResult {
    pub url: String,
    pub title: String,
}

impl super::session::BrowserSession {
    pub async fn navigate(&self, url: &str, timeout_ms: u64) -> Result<NavResult> {
        let page = self
            .browser
            .new_page("about:blank")
            .await
            .map_err(|e| MultizenError::Cdp(format!("new_page: {e}")))?;
        page.goto(url)
            .await
            .map_err(|e| MultizenError::Cdp(format!("goto: {e}")))?;
        // Wait for load with timeout
        let _ = tokio::time::timeout(Duration::from_millis(timeout_ms), async {
            // chromiumoxide waits for load event by default in goto; this is a safety net.
            tokio::time::sleep(Duration::from_millis(100)).await;
        })
        .await;
        let eval = page
            .evaluate("({url: location.href, title: document.title})")
            .await
            .map_err(|e| MultizenError::Cdp(format!("eval: {e}")))?;
        let v: serde_json::Value = eval
            .into_value::<serde_json::Value>()
            .map_err(|e| MultizenError::Cdp(format!("value: {e}")))?;
        Ok(NavResult {
            url: v.get("url").and_then(|x| x.as_str()).unwrap_or("").to_string(),
            title: v.get("title").and_then(|x| x.as_str()).unwrap_or("").to_string(),
        })
    }

    pub async fn screenshot(&self) -> Result<String> {
        let page = self
            .browser
            .pages()
            .await
            .map_err(|e| MultizenError::Cdp(format!("pages: {e}")))?
            .into_iter()
            .next()
            .ok_or_else(|| MultizenError::Cdp("no page".into()))?;
        let bytes = page
            .screenshot(
                ScreenshotParams::builder()
                    .format(CaptureScreenshotFormat::Png)
                    .build(),
            )
            .await
            .map_err(|e| MultizenError::Cdp(format!("screenshot: {e}")))?;
        Ok(base64_encode(&bytes))
    }

    pub async fn evaluate(&self, expression: &str) -> Result<serde_json::Value> {
        let page = self
            .browser
            .pages()
            .await
            .map_err(|e| MultizenError::Cdp(format!("pages: {e}")))?
            .into_iter()
            .next()
            .ok_or_else(|| MultizenError::Cdp("no page".into()))?;
        let eval = page
            .evaluate(expression)
            .await
            .map_err(|e| MultizenError::Cdp(format!("eval: {e}")))?;
        let v: serde_json::Value = eval
            .into_value::<serde_json::Value>()
            .map_err(|e| MultizenError::Cdp(format!("value: {e}")))?;
        Ok(v)
    }

    // click / type / extract require Input domain dispatch + behavioral injection.
    // These are implemented in Task 12 (behavioral integration). Stubs here return
    // NotImplemented-via-error so the compile passes; Task 12 fills them.
    pub async fn click(&self, _selector: &str) -> Result<()> {
        Err(MultizenError::Cdp("click: implemented in Task 12".into()))
    }
    pub async fn type_text(&self, _selector: &str, _text: &str) -> Result<()> {
        Err(MultizenError::Cdp("type: implemented in Task 12".into()))
    }
    pub async fn extract(&self) -> Result<serde_json::Value> {
        Err(MultizenError::Cdp("extract: implemented in Task 12".into()))
    }

    pub async fn close(mut self) {
        let _ = self.browser.close().await;
    }
}

fn base64_encode(input: &[u8]) -> String {
    const TABLE: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut out = String::new();
    let mut i = 0;
    while i + 2 < input.len() {
        let n = ((input[i] as u32) << 16) | ((input[i + 1] as u32) << 8) | (input[i + 2] as u32);
        out.push(TABLE[((n >> 18) & 0x3f) as usize] as char);
        out.push(TABLE[((n >> 12) & 0x3f) as usize] as char);
        out.push(TABLE[((n >> 6) & 0x3f) as usize] as char);
        out.push(TABLE[(n & 0x3f) as usize] as char);
        i += 3;
    }
    let rem = input.len() - i;
    if rem == 1 {
        let n = (input[i] as u32) << 16;
        out.push(TABLE[((n >> 18) & 0x3f) as usize] as char);
        out.push(TABLE[((n >> 12) & 0x3f) as usize] as char);
        out.push('=');
        out.push('=');
    } else if rem == 2 {
        let n = ((input[i] as u32) << 16) | ((input[i + 1] as u32) << 8);
        out.push(TABLE[((n >> 18) & 0x3f) as usize] as char);
        out.push(TABLE[((n >> 12) & 0x3f) as usize] as char);
        out.push(TABLE[((n >> 6) & 0x3f) as usize] as char);
        out.push('=');
    }
    out
}
