use std::path::Path;
use std::process::Stdio;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;

use multizen_core::{ChromixSettings, MultizenError, Profile, Result};
use serde_json::{Value, json};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::TcpListener;
use tokio::process::{Child, ChildStdin, Command};
use tokio::sync::oneshot;
use tokio::task::JoinHandle;

const START_TIMEOUT: Duration = Duration::from_secs(15 * 60);
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
    })
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
    let mut payload = serde_json::to_vec(&request(
        profile,
        binary_path,
        companion_dir,
        config,
        port,
        skip_download,
    ))?;
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
                .ok_or_else(|| launch_error("bridge exited before ready; see SDK stderr"))?;
            let event: Value = serde_json::from_str(&line).map_err(|_| {
                launch_error("invalid bridge ready JSON; stdout is reserved for control")
            })?;
            match event["type"].as_str() {
                Some("ready") if event["cdpEndpoint"].as_str() == Some(&expected_endpoint) => {
                    Ok(())
                }
                Some("error") => Err(launch_error(
                    event["message"].as_str().unwrap_or("SDK launch failed"),
                )),
                _ => Err(launch_error(
                    "bridge closed or returned an unexpected CDP endpoint before ready",
                )),
            }
        };
        let result = tokio::select! {
            result = tokio::time::timeout(START_TIMEOUT, startup) => {
                result.unwrap_or_else(|_| Err(launch_error("startup timed out after 15 minutes (including SDK download)")))
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
