use std::path::Path;
use std::process::Stdio;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

use multizen_core::{ChromixSettings, MultizenError, Profile, Result};
use serde_json::{json, Value};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::TcpListener;
use tokio::process::{Child, ChildStdin, Command};
use tokio::sync::oneshot;
use tokio::task::JoinHandle;

const START_TIMEOUT: Duration = Duration::from_secs(120);
const CLOSE_TIMEOUT: Duration = Duration::from_secs(12);

pub(crate) struct ChromixProcess {
    close: Option<oneshot::Sender<()>>,
    task: Option<JoinHandle<()>>,
    alive: Arc<AtomicBool>,
    pub pid: u32,
    pub endpoint: String,
}

impl ChromixProcess {
    pub fn is_alive(&self) -> bool {
        self.alive.load(Ordering::Acquire)
    }

    pub async fn close(mut self) {
        if let Some(close) = self.close.take() {
            let _ = close.send(());
        }
        if let Some(task) = self.task.take() {
            let _ = task.await;
        }
    }
}

impl Drop for ChromixProcess {
    fn drop(&mut self) {
        if let Some(close) = self.close.take() {
            let _ = close.send(());
        }
    }
}

fn launch_error(message: impl std::fmt::Display) -> MultizenError {
    MultizenError::Launch(format!("Chromix: {message}"))
}

fn request(
    profile: &Profile,
    binary_path: &Path,
    companion_dir: Option<&Path>,
    config: &ChromixSettings,
    port: u16,
    skip_download: bool,
) -> Value {
    let mut extensions = Vec::new();
    if let Some(dir) = companion_dir.filter(|dir| dir.is_dir()) {
        extensions.push(dir.to_string_lossy().into_owned());
    }
    if let Some(configured) = &profile.extensions {
        extensions.extend(
            configured
                .iter()
                .filter(|extension| extension.enabled && Path::new(&extension.dir).is_dir())
                .map(|extension| extension.dir.clone()),
        );
    }
    let proxy = profile.proxy.as_ref().map(|proxy| {
        let host = if proxy.host.contains(':') && !proxy.host.starts_with('[') {
            format!("[{}]", proxy.host)
        } else {
            proxy.host.clone()
        };
        let mut value =
            json!({ "server": format!("{}://{host}:{}", proxy.proxy_type, proxy.port) });
        if let Some(username) = &proxy.username {
            value["username"] = json!(username);
        }
        if let Some(password) = &proxy.password {
            value["password"] = json!(password);
        }
        value
    });
    json!({
        "type": "launch",
        "options": config.options,
        "binaryPath": binary_path.to_string_lossy(),
        "skipDownload": skip_download,
        "cdpPort": port,
        "userDataDir": Path::new(&profile.data_dir).join("engines").join("chromix"),
        "proxy": proxy,
        "extensionPaths": extensions,
        "startUrl": profile.start_url,
        "profileId": profile.id,
        "fingerprint": profile.fingerprint,
    })
}

/// Resolve proxy precedence before adapting authenticated SOCKS to loopback.
async fn bridge_proxy(request: &mut Value) -> Result<Option<crate::socks5_bridge::Socks5Bridge>> {
    let mut pointer = "/proxy";
    let mut explicit = false;
    let mut raw_proxy = false;
    for path in [
        "/options",
        "/options/launchOptions",
        "/options/contextOptions",
    ] {
        if let Some(layer) = request.pointer(path) {
            if layer.get("proxy").is_some() {
                pointer = match path {
                    "/options" => "/options/proxy",
                    "/options/launchOptions" => "/options/launchOptions/proxy",
                    _ => "/options/contextOptions/proxy",
                };
                explicit = true;
            }
            raw_proxy |= layer["args"].as_array().is_some_and(|args| {
                args.iter().any(|arg| {
                    arg.as_str().is_some_and(|arg| {
                        let key = arg.trim().split(['=', ' ']).next().unwrap_or("");
                        matches!(
                            key,
                            "--proxy-server" | "--proxy-pac-url" | "--no-proxy-server"
                        )
                    })
                })
            });
        }
    }
    if raw_proxy && !explicit {
        return Ok(None);
    }
    let Some(proxy) = request.pointer(pointer) else {
        return Ok(None);
    };
    let server = proxy.as_str().or_else(|| proxy["server"].as_str());
    let Some(server) = server else {
        return Ok(None);
    };
    let Ok(url) = reqwest::Url::parse(server) else {
        return Ok(None);
    };
    if url.scheme() != "socks5" {
        return Ok(None);
    }
    let username = proxy["username"]
        .as_str()
        .filter(|s| !s.is_empty())
        .unwrap_or(url.username());
    let password = proxy["password"]
        .as_str()
        .or_else(|| url.password())
        .unwrap_or("");
    if username.is_empty() && password.is_empty() {
        return Ok(None);
    }
    // URL credentials must not be mistaken for their percent-encoded spelling.
    if (proxy.is_string() || !url.username().is_empty())
        && (username.contains('%') || password.contains('%'))
    {
        return Err(launch_error("For SOCKS5 URL credentials containing escapes, use a proxy object with decoded username/password fields"));
    }
    let upstream = multizen_core::ProxyConfig {
        proxy_type: "socks5".into(),
        host: url
            .host_str()
            .ok_or_else(|| launch_error("SOCKS5 proxy requires a host"))?
            .trim_matches(['[', ']'])
            .into(),
        port: url.port().unwrap_or(1080),
        username: Some(username.into()),
        password: Some(password.into()),
    };
    let (bridge, port) = crate::socks5_bridge::Socks5Bridge::start(upstream).await?;
    let mut replacement = json!({"server": format!("socks5://127.0.0.1:{port}")});
    if let Some(bypass) = proxy.get("bypass") {
        replacement["bypass"] = bypass.clone();
    }
    *request.pointer_mut(pointer).unwrap() = replacement;
    Ok(Some(bridge))
}

async fn shutdown(child: &mut Child, input: &mut Option<ChildStdin>) {
    if let Some(mut input) = input.take() {
        let _ = tokio::time::timeout(Duration::from_secs(1), async {
            input.write_all(b"{\"type\":\"close\"}\n").await?;
            input.shutdown().await
        })
        .await;
    }
    if tokio::time::timeout(CLOSE_TIMEOUT, child.wait())
        .await
        .is_err()
    {
        // Playwright cleans up its browser process group on normal Node exit.
        #[cfg(unix)]
        if let Some(pid) = child.id() {
            let _ = Command::new("/bin/kill")
                .args(["-TERM", &pid.to_string()])
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .status()
                .await;
            if tokio::time::timeout(CLOSE_TIMEOUT, child.wait())
                .await
                .is_ok()
            {
                return;
            }
        }
        #[cfg(windows)]
        if let Some(pid) = child.id() {
            let _ = Command::new("taskkill")
                .args(["/PID", &pid.to_string(), "/T", "/F"])
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .status()
                .await;
        }
        let _ = child.kill().await;
    }
}

pub(crate) async fn start(
    profile: &Profile,
    binary_path: &Path,
    companion_dir: Option<&Path>,
    config: &ChromixSettings,
    runtime_dir: &Path,
    skip_download: bool,
) -> Result<ChromixProcess> {
    let script = runtime_dir.join("bridge.mjs");
    if !script.is_file() {
        return Err(launch_error(format!(
            "bridge not found at {}; install the bundled npm runtime with npm ci",
            script.display()
        )));
    }
    let script = std::fs::canonicalize(script).map_err(launch_error)?;
    let reservation = TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, 0))
        .await
        .map_err(launch_error)?;
    let port = reservation.local_addr().map_err(launch_error)?.port();
    let endpoint = format!("http://127.0.0.1:{port}");
    let mut launch_request = request(
        profile,
        binary_path,
        companion_dir,
        config,
        port,
        skip_download,
    );
    let proxy_bridge = bridge_proxy(&mut launch_request).await?;
    let mut payload = serde_json::to_vec(&launch_request)?;
    payload.push(b'\n');
    let mut command = Command::new(&config.node_path);
    command
        .arg(script)
        .envs(&config.environment)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .kill_on_drop(true);
    #[cfg(windows)]
    command.creation_flags(0x08000000);
    let mut child = command.spawn().map_err(|error| {
        launch_error(format!(
            "cannot start Node runtime (Node >=20 required): {error}"
        ))
    })?;
    let pid = child.id().ok_or_else(|| launch_error("missing Node pid"))?;
    let mut input = child.stdin.take();
    let stdout = child
        .stdout
        .take()
        .ok_or_else(|| launch_error("missing bridge stdout"))?;
    let (close_tx, mut close_rx) = oneshot::channel();
    let (ready_tx, ready_rx) = oneshot::channel();
    let alive = Arc::new(AtomicBool::new(false));
    let running = Arc::clone(&alive);
    let expected_endpoint = endpoint.clone();
    // Chromium must bind the reserved port itself; validate its actual endpoint before registering.
    drop(reservation);
    let task = tokio::spawn(async move {
        let mut lines = BufReader::new(stdout).lines();
        let startup = async {
            input
                .as_mut()
                .ok_or_else(|| launch_error("missing bridge stdin"))?
                .write_all(&payload)
                .await
                .map_err(launch_error)?;
            let line = lines
                .next_line()
                .await
                .map_err(launch_error)?
                .ok_or_else(|| launch_error("bridge exited before ready; see Playwright stderr"))?;
            let event: Value = serde_json::from_str(&line).map_err(|_| {
                launch_error("invalid bridge ready JSON; stdout is reserved for control")
            })?;
            match event["type"].as_str() {
                Some("ready") if event["cdpEndpoint"].as_str() == Some(&expected_endpoint) => {
                    Ok(())
                }
                Some("error") => Err(launch_error(
                    event["message"]
                        .as_str()
                        .unwrap_or("Playwright launch failed"),
                )),
                _ => Err(launch_error(
                    "bridge closed or returned an unexpected CDP endpoint before ready",
                )),
            }
        };
        let result = tokio::select! {
            result = tokio::time::timeout(START_TIMEOUT, startup) => {
                result.unwrap_or_else(|_| Err(launch_error("startup timed out after 120 seconds; check the local browser executable")))
            }
            _ = &mut close_rx => Err(launch_error("launch cancelled")),
        };
        let started = result.is_ok();
        running.store(started, Ordering::Release);
        if ready_tx.send(result).is_ok() && started {
            loop {
                tokio::select! {
                    _ = &mut close_rx => break,
                    line = lines.next_line() => {
                        match line {
                            Ok(Some(line)) => {
                                let event = serde_json::from_str::<Value>(&line);
                                if !matches!(event, Ok(ref value) if value["type"] == "ready") {
                                    break;
                                }
                            }
                            _ => break,
                        }
                    }
                    _ = child.wait() => break,
                }
            }
        }
        running.store(false, Ordering::Release);
        shutdown(&mut child, &mut input).await;
        if let Some(bridge) = proxy_bridge {
            let _ = bridge.stop().await;
        }
    });
    let process = ChromixProcess {
        close: Some(close_tx),
        task: Some(task),
        alive,
        pid,
        endpoint,
    };
    match ready_rx.await {
        Ok(Ok(())) => Ok(process),
        result => {
            process.close().await;
            Err(match result {
                Ok(Err(error)) => error,
                _ => launch_error("bridge supervisor stopped before ready"),
            })
        }
    }
}
