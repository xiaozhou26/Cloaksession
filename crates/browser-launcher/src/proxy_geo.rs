use serde::Deserialize;
use std::time::Duration;

use multizen_core::{MultizenError, ProxyConfig, Result};

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProxyGeoResult {
    pub country: String,
    pub country_name: String,
    pub timezone: String,
    pub city: String,
    pub ip: String,
    pub latitude: Option<f64>,
    pub longitude: Option<f64>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
struct IpApiResp {
    country_code: Option<String>,
    country_name: Option<String>,
    timezone: Option<String>,
    city: Option<String>,
    ip: Option<String>,
    latitude: Option<serde_json::Value>,
    longitude: Option<serde_json::Value>,
    error: Option<String>,
    reason: Option<String>,
}

/// Pure parse of an ipapi.co /json/ response body. Separated from the HTTP
/// call so it can be unit-tested without network.
pub fn parse_ipapi_response(body: &str) -> Result<ProxyGeoResult> {
    let resp: IpApiResp = serde_json::from_str(body)
        .map_err(|e| MultizenError::Config(format!("ipapi parse: {e}")))?;
    if let Some(err) = resp.error {
        let reason = resp.reason.unwrap_or_default();
        return Err(MultizenError::Config(format!("ipapi.co error: {err} - {reason}")));
    }
    let country_code =
        resp.country_code
            .ok_or_else(|| MultizenError::Config("ipapi: missing country_code".into()))?;
    let timezone =
        resp.timezone
            .ok_or_else(|| MultizenError::Config("ipapi: missing timezone".into()))?;
    let as_f64 = |v: Option<serde_json::Value>| match v {
        Some(serde_json::Value::Number(n)) => n.as_f64(),
        _ => None,
    };
    Ok(ProxyGeoResult {
        country: country_code.to_lowercase(),
        country_name: resp.country_name.unwrap_or(country_code),
        timezone,
        city: resp.city.unwrap_or_default(),
        ip: resp.ip.unwrap_or_default(),
        latitude: as_f64(resp.latitude),
        longitude: as_f64(resp.longitude),
    })
}

pub async fn probe_proxy_geo(proxy: &ProxyConfig, timeout_ms: u64) -> Result<ProxyGeoResult> {
    let client = build_client(proxy, timeout_ms)?;
    let resp = client
        .get("https://ipapi.co/json/")
        .header("user-agent", "Cloaksession/0.2 (proxy-geo-probe)")
        .header("accept", "application/json")
        .send()
        .await
        .map_err(|e| MultizenError::Config(format!("ipapi request: {e}")))?;
    let body = resp
        .text()
        .await
        .map_err(|e| MultizenError::Config(format!("ipapi body: {e}")))?;
    parse_ipapi_response(&body)
}

fn build_client(proxy: &ProxyConfig, timeout_ms: u64) -> Result<reqwest::Client> {
    let url = if proxy.proxy_type == "socks5" {
        format!("socks5://{}:{}", proxy.host, proxy.port)
    } else {
        format!("http://{}:{}", proxy.host, proxy.port)
    };
    let mut req = reqwest::Client::builder().timeout(Duration::from_millis(timeout_ms));
    if let (Some(u), Some(p)) = (&proxy.username, &proxy.password) {
        req = req.proxy(
            reqwest::Proxy::all(&url)
                .map_err(|e| MultizenError::Config(format!("proxy: {e}")))?
                .basic_auth(u, p),
        );
    } else {
        req = req.proxy(
            reqwest::Proxy::all(&url)
                .map_err(|e| MultizenError::Config(format!("proxy: {e}")))?,
        );
    }
    req.build()
        .map_err(|e| MultizenError::Config(format!("client: {e}")))
}
