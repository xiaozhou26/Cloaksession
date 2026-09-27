use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use browser_launcher::BrowserLauncher;
use multizen_core::{BrowserEngine, ChromixSettings, CreateProfileInput, Profile, ProxyConfig};
use profile_manager::ProfileManager;
use serde_json::{Value, json};
use tempfile::TempDir;

struct Fixture {
    directory: TempDir,
    pm: Arc<ProfileManager>,
    profile: Profile,
    launcher: BrowserLauncher,
    config: ChromixSettings,
}

impl Fixture {
    fn new() -> Self {
        let directory = TempDir::new().unwrap();
        let bridge =
            Path::new(env!("CARGO_MANIFEST_DIR")).join("../tauri-app/resources/chromix/bridge.mjs");
        let bridge_url = format!(
            "file://{}",
            std::fs::canonicalize(bridge).unwrap().display()
        );
        let script = format!(
            "import {{ runBridge }} from {};\n{}",
            serde_json::to_string(&bridge_url).unwrap(),
            r#"
import { writeFileSync } from 'node:fs';
import { EventEmitter } from 'node:events';
const write = process.stdout.write.bind(process.stdout);
process.stdout.write = process.stderr.write.bind(process.stderr);
const context = new EventEmitter();
context.pages = () => [{ url: () => 'about:blank', goto: async () => {} }];
context.close = async () => {
  writeFileSync(process.env.CLOSE_FILE, 'closed');
  context.emit('close');
};
await runBridge({
  send: (event) => write(JSON.stringify(event) + '\n'),
  ready: (_, signal) => new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, Number(process.env.READY_DELAY || 0));
    signal.addEventListener('abort', () => { clearTimeout(timer); reject(new Error('cancelled')); });
  }),
  loadSdk: async () => ({
    launchPersistentContext: async (options) => {
      writeFileSync(process.env.CAPTURE_FILE, JSON.stringify({
        options, argv: process.argv, secret: process.env.BRIDGE_SECRET,
      }));
      console.log('fake SDK log goes to stderr');
      if (process.env.FAIL_LAUNCH) throw new Error('intentional fake SDK failure');
      if (process.env.CLOSE_AFTER) setTimeout(() => context.emit('close'), Number(process.env.CLOSE_AFTER));
      return context;
    },
  }),
  forceExit: (code) => process.exit(code),
});
process.exit(0);
"#
        );
        std::fs::write(directory.path().join("bridge.mjs"), script).unwrap();
        #[allow(clippy::arc_with_non_send_sync)]
        let pm = Arc::new(
            ProfileManager::new(
                &directory.path().join("profiles.db"),
                &directory.path().join("profiles"),
            )
            .unwrap(),
        );
        let profile = pm
            .create(CreateProfileInput {
                name: "Chromix fake SDK".into(),
                proxy: Some(ProxyConfig {
                    proxy_type: "http".into(),
                    host: "proxy.invalid".into(),
                    port: 8080,
                    username: Some("private-user".into()),
                    password: Some("private-password".into()),
                }),
                start_url: Some("https://example.com".into()),
                ..Default::default()
            })
            .unwrap();
        let launcher = BrowserLauncher::new(Arc::clone(&pm));
        let mut config = ChromixSettings::default();
        config.environment.insert(
            "CAPTURE_FILE".into(),
            directory.path().join("capture.json").display().to_string(),
        );
        config.environment.insert(
            "CLOSE_FILE".into(),
            directory.path().join("closed").display().to_string(),
        );
        config
            .environment
            .insert("BRIDGE_SECRET".into(), "private-env-token".into());
        Self {
            directory,
            pm,
            profile,
            launcher,
            config,
        }
    }

    fn runtime(&self) -> &Path {
        self.directory.path()
    }

    fn capture(&self) -> Value {
        serde_json::from_slice(&std::fs::read(self.runtime().join("capture.json")).unwrap())
            .unwrap()
    }

    async fn wait_for_file(&self, name: &str) {
        tokio::time::timeout(Duration::from_secs(3), async {
            while !self.runtime().join(name).exists() {
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
        })
        .await
        .unwrap();
    }
}

#[tokio::test]
async fn persistent_launch_preserves_options_and_keeps_secrets_off_argv() {
    let mut fixture = Fixture::new();
    fixture.config.options = json!({
        "futureSdkField": {"opaque": [true, null, "kept"]},
        "humanConfig": {"seed": 17, "custom": "preserved"},
        "launchOptions": {"slowMo": 2},
        "contextOptions": {"permissions": ["clipboard-read"]},
    })
    .as_object()
    .unwrap()
    .clone();
    let launched = fixture
        .launcher
        .launch_with_chromix(
            &fixture.profile.id,
            Path::new(""),
            None,
            &fixture.config,
            fixture.runtime(),
            false,
        )
        .await
        .unwrap();
    assert!(fixture.launcher.is_running_async(&fixture.profile.id).await);
    assert!(
        fixture
            .pm
            .get(&fixture.profile.id)
            .unwrap()
            .unwrap()
            .last_opened_at
            .is_some()
    );
    let captured = fixture.capture();
    assert_eq!(
        captured["options"]["userDataDir"],
        json!(PathBuf::from(&fixture.profile.data_dir).join("engines/chromix"))
    );
    for (key, value) in &fixture.config.options {
        assert_eq!(&captured["options"][key], value);
    }
    assert_eq!(captured["options"]["proxy"]["password"], "private-password");
    assert_eq!(captured["secret"], "private-env-token");
    let argv = captured["argv"].as_array().unwrap();
    assert_eq!(argv.len(), 2);
    assert!(
        argv.iter()
            .all(|value| !value.as_str().unwrap().contains("private-"))
    );
    let port = launched.cdp_endpoint.rsplit(':').next().unwrap();
    assert_eq!(
        captured["options"]["args"],
        json!([
            "--remote-debugging-address=127.0.0.1",
            format!("--remote-debugging-port={port}"),
        ])
    );
    assert_eq!(captured["options"].get("geoip"), None);
    let again = fixture
        .launcher
        .launch_with_chromix(
            &fixture.profile.id,
            Path::new(""),
            None,
            &fixture.config,
            fixture.runtime(),
            false,
        )
        .await
        .unwrap();
    assert_eq!(again.pid, launched.pid);
    fixture.launcher.close(&fixture.profile.id).await.unwrap();
    assert_eq!(
        std::fs::read_to_string(fixture.runtime().join("closed")).unwrap(),
        "closed"
    );
    assert!(!fixture.launcher.is_running_async(&fixture.profile.id).await);
}

#[tokio::test]
async fn running_registry_waits_for_the_ready_handshake() {
    let mut fixture = Fixture::new();
    fixture
        .config
        .environment
        .insert("READY_DELAY".into(), "200".into());
    let launch = fixture.launcher.launch_with_chromix(
        &fixture.profile.id,
        Path::new(""),
        None,
        &fixture.config,
        fixture.runtime(),
        false,
    );
    tokio::pin!(launch);
    tokio::select! {
        _ = &mut launch => panic!("launch completed before readiness"),
        _ = tokio::time::sleep(Duration::from_millis(50)) => {},
    }
    assert!(!fixture.launcher.is_running_async(&fixture.profile.id).await);
    assert!(
        fixture
            .pm
            .get(&fixture.profile.id)
            .unwrap()
            .unwrap()
            .last_opened_at
            .is_none()
    );
    launch.await.unwrap();
    fixture.launcher.close_all().await;
}

#[tokio::test]
async fn sdk_failure_never_marks_the_profile_running_or_opened() {
    let mut fixture = Fixture::new();
    fixture
        .config
        .environment
        .insert("FAIL_LAUNCH".into(), "1".into());
    let error = fixture
        .launcher
        .launch_with_chromix(
            &fixture.profile.id,
            Path::new(""),
            None,
            &fixture.config,
            fixture.runtime(),
            false,
        )
        .await
        .unwrap_err();
    assert!(error.to_string().contains("intentional fake SDK failure"));
    assert!(!fixture.launcher.is_running_async(&fixture.profile.id).await);
    assert!(
        fixture
            .pm
            .get(&fixture.profile.id)
            .unwrap()
            .unwrap()
            .last_opened_at
            .is_none()
    );
}

#[tokio::test]
async fn cancelling_startup_closes_the_inflight_sdk_context() {
    let mut fixture = Fixture::new();
    fixture
        .config
        .environment
        .insert("READY_DELAY".into(), "30000".into());
    {
        let launch = fixture.launcher.launch_with_chromix(
            &fixture.profile.id,
            Path::new(""),
            None,
            &fixture.config,
            fixture.runtime(),
            false,
        );
        tokio::pin!(launch);
        tokio::select! {
            _ = &mut launch => panic!("launch should still await readiness"),
            _ = fixture.wait_for_file("capture.json") => {},
        }
    }
    fixture.wait_for_file("closed").await;
    assert!(!fixture.launcher.is_running_async(&fixture.profile.id).await);
}

#[tokio::test]
async fn browser_exit_updates_liveness_and_override_profile_dir_is_respected() {
    let mut fixture = Fixture::new();
    fixture
        .config
        .environment
        .insert("CLOSE_AFTER".into(), "100".into());
    let override_dir = fixture.runtime().join("user-override");
    fixture
        .config
        .options
        .insert("userDataDir".into(), json!(override_dir));
    fixture
        .launcher
        .launch_with_chromix(
            &fixture.profile.id,
            Path::new(""),
            None,
            &fixture.config,
            fixture.runtime(),
            false,
        )
        .await
        .unwrap();
    assert_eq!(
        fixture.capture()["options"]["userDataDir"],
        json!(override_dir)
    );
    tokio::time::timeout(Duration::from_secs(3), async {
        while fixture.launcher.is_running_async(&fixture.profile.id).await {
            tokio::time::sleep(Duration::from_millis(10)).await;
        }
    })
    .await
    .unwrap();
    fixture.launcher.close_all().await;
}

#[tokio::test]
async fn missing_runtime_and_legacy_launch_have_actionable_errors() {
    let fixture = Fixture::new();
    let error = fixture
        .launcher
        .launch_with_chromix(
            &fixture.profile.id,
            Path::new(""),
            None,
            &fixture.config,
            &fixture.runtime().join("missing-runtime"),
            false,
        )
        .await
        .unwrap_err();
    assert!(error.to_string().contains("npm ci"));
    let error = fixture
        .launcher
        .launch(
            &fixture.profile.id,
            Path::new(""),
            BrowserEngine::Chromix,
            None,
        )
        .await
        .unwrap_err();
    assert!(error.to_string().contains("launch_with_chromix"));
}
