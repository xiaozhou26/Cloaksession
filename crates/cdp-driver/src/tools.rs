use std::time::Duration;

use behavioral::keyboard::humanized_keystroke_delays;
use behavioral::mouse::humanized_path;
use chromiumoxide::cdp::browser_protocol::input::{
    DispatchKeyEventParams, DispatchKeyEventType, DispatchMouseEventParams,
    DispatchMouseEventType, MouseButton,
};
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

    // click / type / extract require Input domain dispatch + behavioral
    // injection (humanized_path for mouse approach, humanized_keystroke_delays
    // for typing). Implemented per Task 12 with chromiumoxide 0.7 input API.
    pub async fn click(&self, selector: &str) -> Result<()> {
        let page = self
            .browser
            .pages()
            .await
            .map_err(|e| MultizenError::Cdp(format!("pages: {e}")))?
            .into_iter()
            .next()
            .ok_or_else(|| MultizenError::Cdp("no page".into()))?;
        // Find + scrollIntoView + get center.
        let expr = format!(
            r#"(function() {{
                var el = document.querySelector({selector:?});
                if (!el) return null;
                el.scrollIntoView({{block:'center'}});
                var r = el.getBoundingClientRect();
                return {{x: r.x + r.width/2, y: r.y + r.height/2}};
            }})()"#
        );
        let v: serde_json::Value = page
            .evaluate(expr.as_str())
            .await
            .map_err(|e| MultizenError::Cdp(format!("find: {e}")))?
            .into_value::<serde_json::Value>()
            .map_err(|e| MultizenError::Cdp(format!("value: {e}")))?;
        let cx = v
            .get("x")
            .and_then(|x| x.as_f64())
            .ok_or_else(|| MultizenError::Cdp("element not found".into()))?;
        let cy = v
            .get("y")
            .and_then(|y| y.as_f64())
            .ok_or_else(|| MultizenError::Cdp("element not found".into()))?;

        let seed = (cx.to_bits() ^ cy.to_bits()) as u64;
        // Short humanized approach path (mouseMoved events).
        for (x, y) in humanized_path((cx - 4.0, cy - 4.0), (cx, cy), seed) {
            let cmd = DispatchMouseEventParams::builder()
                .x(x)
                .y(y)
                .button(MouseButton::None)
                .r#type(DispatchMouseEventType::MouseMoved)
                .build()
                .map_err(|e| MultizenError::Cdp(format!("build: {e}")))?;
            let _ = page.execute(cmd).await;
        }
        // Press.
        let press = DispatchMouseEventParams::builder()
            .x(cx)
            .y(cy)
            .button(MouseButton::Left)
            .click_count(1)
            .r#type(DispatchMouseEventType::MousePressed)
            .build()
            .map_err(|e| MultizenError::Cdp(format!("build: {e}")))?;
        let _ = page.execute(press).await;
        // Release.
        let release = DispatchMouseEventParams::builder()
            .x(cx)
            .y(cy)
            .button(MouseButton::Left)
            .click_count(1)
            .r#type(DispatchMouseEventType::MouseReleased)
            .build()
            .map_err(|e| MultizenError::Cdp(format!("build: {e}")))?;
        let _ = page.execute(release).await;
        Ok(())
    }

    pub async fn type_text(&self, selector: &str, text: &str) -> Result<()> {
        let page = self
            .browser
            .pages()
            .await
            .map_err(|e| MultizenError::Cdp(format!("pages: {e}")))?
            .into_iter()
            .next()
            .ok_or_else(|| MultizenError::Cdp("no page".into()))?;
        // Focus the target element.
        let focus_expr = format!(
            r#"(function(){{var el=document.querySelector({selector:?});if(el){{el.focus();return true;}}return false;}})()"#
        );
        let _ = page.evaluate(focus_expr.as_str()).await;

        let seed = text.len() as u64;
        let delays = humanized_keystroke_delays(text, seed);
        for (i, ch) in text.chars().enumerate() {
            let key_down = DispatchKeyEventParams::builder()
                .r#type(DispatchKeyEventType::KeyDown)
                .text(ch.to_string())
                .key(ch.to_string())
                .build()
                .map_err(|e| MultizenError::Cdp(format!("build: {e}")))?;
            let _ = page.execute(key_down).await;
            let key_up = DispatchKeyEventParams::builder()
                .r#type(DispatchKeyEventType::KeyUp)
                .key(ch.to_string())
                .build()
                .map_err(|e| MultizenError::Cdp(format!("build: {e}")))?;
            let _ = page.execute(key_up).await;
            if let Some(ms) = delays.get(i) {
                tokio::time::sleep(Duration::from_millis(*ms)).await;
            }
        }
        Ok(())
    }

    pub async fn extract(&self) -> Result<serde_json::Value> {
        let page = self
            .browser
            .pages()
            .await
            .map_err(|e| MultizenError::Cdp(format!("pages: {e}")))?
            .into_iter()
            .next()
            .ok_or_else(|| MultizenError::Cdp("no page".into()))?;
        let meta = page
            .evaluate("({url: location.href, title: document.title})")
            .await
            .map_err(|e| MultizenError::Cdp(format!("meta: {e}")))?
            .into_value::<serde_json::Value>()
            .map_err(|e| MultizenError::Cdp(format!("value: {e}")))?;
        // innerText fallback (full a11y tree extraction requires the
        // Accessibility domain which is gated behind safe-enable; using
        // innerText keeps the integration testable without CloakBrowser
        // DCHECK risk).
        let inner = page
            .evaluate("document.body ? document.body.innerText.slice(0,8000) : ''")
            .await
            .map_err(|e| MultizenError::Cdp(format!("innerText: {e}")))?
            .into_value::<serde_json::Value>()
            .map_err(|e| MultizenError::Cdp(format!("value: {e}")))?;
        Ok(serde_json::json!({
            "url": meta.get("url"),
            "title": meta.get("title"),
            "text": inner
        }))
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
