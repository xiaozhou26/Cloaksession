use serde_json::{json, Value};
use std::io::{BufRead, BufReader, Write};
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::mpsc::{self, Receiver};
use std::time::{Duration, Instant};
use tempfile::TempDir;

struct Core {
    child: Child,
    stdin: Option<ChildStdin>,
    messages: Receiver<Value>,
    data: TempDir,
}

impl Core {
    fn start() -> Self {
        let data = TempDir::new().unwrap();
        std::fs::write(
            data.path().join("settings.json"),
            r#"{"mcpHttpEnabled":false,"autoUpdate":false}"#,
        )
        .unwrap();
        let mut child = Command::new(env!("CARGO_BIN_EXE_desktop-core"))
            .env("CLOAKSESSION_DATA_DIR", data.path())
            .env("CLOAKSESSION_RESOURCE_DIR", data.path().join("resources"))
            .env("RUST_LOG", "info")
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .unwrap();
        let stdin = child.stdin.take();
        let stdout = child.stdout.take().unwrap();
        let (sender, messages) = mpsc::channel();
        std::thread::spawn(move || {
            for line in BufReader::new(stdout).lines() {
                let line = line.unwrap();
                let message = serde_json::from_str(&line)
                    .unwrap_or_else(|error| panic!("Non-protocol stdout: {line}: {error}"));
                if sender.send(message).is_err() {
                    break;
                }
            }
        });
        let core = Self {
            child,
            stdin,
            messages,
            data,
        };
        assert_eq!(core.receive(), json!({"event":"core:ready","data":null}));
        assert!(core.data.path().join("profiles.db").exists());
        assert!(core.data.path().join("mcp-token").exists());
        core
    }

    fn send(&mut self, message: Value) {
        self.raw(&message.to_string());
    }

    fn raw(&mut self, line: &str) {
        writeln!(self.stdin.as_mut().unwrap(), "{line}").unwrap();
        self.stdin.as_mut().unwrap().flush().unwrap();
    }

    fn receive(&self) -> Value {
        self.messages
            .recv_timeout(Duration::from_secs(10))
            .expect("core response timeout")
    }

    fn request(&mut self, id: u64, command: &str, args: Value) -> Value {
        self.send(json!({"id":id,"command":command,"args":args}));
        let response = self.receive();
        assert_eq!(response["id"], id);
        response
    }

    fn wait_for_exit(&mut self) {
        let deadline = Instant::now() + Duration::from_secs(6);
        loop {
            if let Some(status) = self.child.try_wait().unwrap() {
                assert!(status.success(), "core exit: {status}");
                break;
            }
            assert!(Instant::now() < deadline, "core did not stop");
            std::thread::sleep(Duration::from_millis(20));
        }
    }
}

impl Drop for Core {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

#[test]
fn commands_preserve_results_errors_and_camel_case_arguments() {
    let mut core = Core::start();
    let info = core.request(1, "system_info", json!({}));
    assert_eq!(info["result"]["appVersion"], "1.3.0");
    assert_eq!(
        core.request(2, "profiles_list", json!({}))["result"],
        json!([])
    );
    let created = core.request(3, "profiles_create", json!({"input":{"name":"RPC test"}}));
    let id = created["result"]["id"].as_str().unwrap();
    assert_eq!(
        core.request(4, "profiles_get", json!({"id":id}))["result"]["name"],
        "RPC test"
    );
    assert_eq!(
        core.request(5, "extensions_list", json!({"profileId":id}))["result"],
        json!([])
    );
    assert_eq!(
        core.request(6, "profiles_delete", json!({"id":id}))["result"],
        Value::Null
    );
    assert_eq!(
        core.request(7, "activity_recent", json!({}))["result"],
        json!([])
    );
    assert!(core.request(8, "does_not_exist", json!({}))["error"]
        .as_str()
        .unwrap()
        .contains("Unknown command"));
    assert!(core.request(9, "profiles_get", json!({}))["error"]
        .as_str()
        .unwrap()
        .contains("id"));
    assert!(core.request(10, "profiles_get", json!({"id":42}))["error"].is_string());
    assert!(core.request(11, "profiles_list", json!([]))["error"].is_string());
    core.raw("not JSON");
    let malformed = core.receive();
    assert_eq!(malformed["id"], Value::Null);
    assert!(malformed["error"]
        .as_str()
        .unwrap()
        .contains("Invalid JSON"));
    assert_eq!(
        core.request(12, "shutdown", json!({})),
        json!({"id":12,"result":null})
    );
    // Keep stdin open to verify shutdown does not wait for the reader thread.
    core.wait_for_exit();
}

#[test]
fn dialogs_do_not_block_commands_and_propagate_results_cancellation_and_errors() {
    let mut core = Core::start();
    core.send(json!({"id":1,"command":"dialog_pick_browser_binary","args":{}}));
    let dialog = core.receive();
    assert_eq!(dialog["id"], 1);
    assert_eq!(
        dialog["dialog"],
        json!({"kind":"open","filters":[{"displayName":"Browser binary","pattern":"*.exe;*.app;*.sh"}]})
    );
    assert_eq!(
        core.request(2, "profiles_list", json!({}))["result"],
        json!([])
    );
    core.send(json!({"id":dialog["id"],"result":"/tmp/browser"}));
    assert_eq!(core.receive(), json!({"id":1,"result":"/tmp/browser"}));

    core.send(json!({"id":3,"command":"dialog_pick_directory","args":{}}));
    let dialog = core.receive();
    assert_eq!(dialog["dialog"]["kind"], "directory");
    core.send(json!({"id":dialog["id"],"result":null}));
    assert_eq!(core.receive(), json!({"id":3,"result":null}));

    core.send(json!({"id":4,"command":"extensions_prepare_from_file","args":{}}));
    let dialog = core.receive();
    core.send(json!({"id":dialog["id"],"result":null,"error":"Host dialog failed"}));
    assert_eq!(core.receive(), json!({"id":4,"error":"Host dialog failed"}));

    core.stdin.take();
    core.wait_for_exit();
}

#[test]
fn eof_stops_core_even_with_a_pending_dialog() {
    let mut core = Core::start();
    core.send(json!({"id":1,"command":"dialog_pick_directory","args":{}}));
    assert_eq!(core.receive()["dialog"]["kind"], "directory");
    core.stdin.take();
    core.wait_for_exit();
}

#[test]
fn archive_save_dialog_preserves_default_filename_and_cancellation() {
    let mut core = Core::start();
    let created = core.request(
        1,
        "profiles_create",
        json!({"input":{"name":"Archive Test"}}),
    );
    core.send(json!({"id":2,"command":"profiles_export_archive","args":{
        "id":created["result"]["id"],"passphrase":"test-passphrase"
    }}));
    let dialog = core.receive();
    assert_eq!(
        dialog["dialog"],
        json!({"kind":"save","filters":[{
        "displayName":"Cloaksession archive","pattern":"*.mzar"
    }],"defaultFilename":"ArchiveTest.mzar"})
    );
    core.send(json!({"id":dialog["id"],"result":null}));
    assert_eq!(
        core.receive(),
        json!({"id":2,"result":{"ok":false,"reason":"cancelled"}})
    );
    core.stdin.take();
    core.wait_for_exit();
}
