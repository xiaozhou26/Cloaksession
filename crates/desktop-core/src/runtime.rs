//! Host transport shared by command handlers and background event producers.

use serde::Serialize;
use serde_json::{json, Value};
use std::collections::HashMap;
use std::io::Write;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use tokio::sync::oneshot;

pub type State<'a, T> = &'a T;
type DialogResult = Result<Option<PathBuf>, String>;

struct Dialogs {
    next_id: u64,
    pending: HashMap<u64, oneshot::Sender<DialogResult>>,
    closed: bool,
}

struct Transport {
    writer: Mutex<Box<dyn Write + Send>>,
    dialogs: Mutex<Dialogs>,
}

#[derive(Clone)]
pub struct AppHandle(Arc<Transport>);

impl AppHandle {
    pub fn new(writer: impl Write + Send + 'static) -> Self {
        Self(Arc::new(Transport {
            writer: Mutex::new(Box::new(writer)),
            dialogs: Mutex::new(Dialogs {
                next_id: 1,
                pending: HashMap::new(),
                closed: false,
            }),
        }))
    }

    pub fn send(&self, message: &Value) -> Result<(), String> {
        let mut bytes = serde_json::to_vec(message).map_err(|error| error.to_string())?;
        bytes.push(b'\n');
        let mut writer = self.0.writer.lock().map_err(|error| error.to_string())?;
        writer
            .write_all(&bytes)
            .and_then(|_| writer.flush())
            .map_err(|error| error.to_string())
    }

    pub fn emit<T: Serialize>(&self, event: &str, data: T) -> Result<(), String> {
        let data = serde_json::to_value(data).map_err(|error| error.to_string())?;
        self.send(&json!({ "event": event, "data": data }))
    }

    pub fn reply(&self, id: Value, result: Result<Value, String>) -> Result<(), String> {
        let message = match result {
            Ok(result) => json!({ "id": id, "result": result }),
            Err(error) => json!({ "id": id, "error": error }),
        };
        self.send(&message)
    }

    pub fn dialog(&self) -> DialogBuilder {
        DialogBuilder {
            app: self.clone(),
            request: DialogRequest {
                kind: "open",
                title: None,
                filters: Vec::new(),
                default_filename: None,
            },
        }
    }

    pub fn resolve_dialog(&self, message: &Value) -> Result<(), String> {
        let id = message
            .get("id")
            .and_then(Value::as_u64)
            .ok_or("Dialog response id must be an unsigned integer")?;
        let sender = self
            .0
            .dialogs
            .lock()
            .unwrap()
            .pending
            .remove(&id)
            .ok_or_else(|| format!("Unknown dialog response id: {id}"))?;
        let result = match message.get("error") {
            Some(Value::String(error)) => Err(error.clone()),
            Some(value) if !value.is_null() => Err("Dialog error must be a string".into()),
            _ => match message.get("result") {
                Some(Value::Null) => Ok(None),
                Some(Value::String(path)) => Ok(Some(PathBuf::from(path))),
                _ => Err("Dialog result must be a string or null".into()),
            },
        };
        let _ = sender.send(result);
        Ok(())
    }

    pub fn close_dialogs(&self) {
        let mut dialogs = self.0.dialogs.lock().unwrap();
        dialogs.closed = true;
        for (_, sender) in dialogs.pending.drain() {
            let _ = sender.send(Err("Desktop host disconnected".into()));
        }
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct DialogRequest {
    kind: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    title: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    filters: Vec<DialogFilter>,
    #[serde(skip_serializing_if = "Option::is_none")]
    default_filename: Option<String>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct DialogFilter {
    display_name: String,
    pattern: String,
}

pub struct DialogBuilder {
    app: AppHandle,
    request: DialogRequest,
}

impl DialogBuilder {
    pub fn file(self) -> Self {
        self
    }

    pub fn add_filter(mut self, name: &str, extensions: &[&str]) -> Self {
        self.request.filters.push(DialogFilter {
            display_name: name.into(),
            pattern: extensions
                .iter()
                .map(|ext| {
                    if *ext == "*" {
                        "*".into()
                    } else {
                        format!("*.{ext}")
                    }
                })
                .collect::<Vec<_>>()
                .join(";"),
        });
        self
    }

    pub fn set_title(mut self, title: impl Into<String>) -> Self {
        self.request.title = Some(title.into());
        self
    }

    pub fn set_file_name(mut self, name: impl Into<String>) -> Self {
        self.request.default_filename = Some(name.into());
        self
    }

    pub async fn pick_file(self) -> DialogResult {
        self.request("open").await
    }

    pub async fn pick_folder(self) -> DialogResult {
        self.request("directory").await
    }

    pub async fn save_file(self) -> DialogResult {
        self.request("save").await
    }

    async fn request(mut self, kind: &'static str) -> DialogResult {
        self.request.kind = kind;
        let (sender, receiver) = oneshot::channel();
        let id = {
            let mut dialogs = self.app.0.dialogs.lock().unwrap();
            if dialogs.closed {
                return Err("Desktop host disconnected".into());
            }
            let id = dialogs.next_id;
            dialogs.next_id += 1;
            dialogs.pending.insert(id, sender);
            id
        };
        let guard = PendingDialog {
            app: self.app.clone(),
            id,
        };
        self.app
            .send(&json!({ "id": id, "dialog": self.request }))?;
        let result = receiver
            .await
            .map_err(|_| "Desktop host disconnected".to_string())?;
        drop(guard);
        result
    }
}

struct PendingDialog {
    app: AppHandle,
    id: u64,
}

impl Drop for PendingDialog {
    fn drop(&mut self) {
        self.app.0.dialogs.lock().unwrap().pending.remove(&self.id);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Clone, Default)]
    struct Buffer(Arc<Mutex<Vec<u8>>>);

    impl Write for Buffer {
        fn write(&mut self, bytes: &[u8]) -> std::io::Result<usize> {
            self.0.lock().unwrap().extend_from_slice(bytes);
            Ok(bytes.len())
        }

        fn flush(&mut self) -> std::io::Result<()> {
            Ok(())
        }
    }

    #[test]
    fn concurrent_events_are_complete_json_lines() {
        let buffer = Buffer::default();
        let app = AppHandle::new(buffer.clone());
        std::thread::scope(|scope| {
            for index in 0..16 {
                let app = app.clone();
                scope.spawn(move || app.emit("test:event", json!({"index":index})).unwrap());
            }
        });
        let output = String::from_utf8(buffer.0.lock().unwrap().clone()).unwrap();
        assert_eq!(output.lines().count(), 16);
        for line in output.lines() {
            let message: Value = serde_json::from_str(line).unwrap();
            assert_eq!(message["event"], "test:event");
            assert!(message["data"]["index"].is_number());
        }
    }

    #[tokio::test]
    async fn host_disconnect_releases_pending_and_future_dialogs() {
        let app = AppHandle::new(std::io::sink());
        let pending = app.dialog().file().pick_folder();
        tokio::pin!(pending);
        assert!(futures_util::poll!(&mut pending).is_pending());
        app.close_dialogs();
        assert_eq!(pending.await.unwrap_err(), "Desktop host disconnected");
        assert_eq!(
            app.dialog().file().pick_file().await.unwrap_err(),
            "Desktop host disconnected"
        );
        assert!(app.0.dialogs.lock().unwrap().pending.is_empty());
    }

    #[tokio::test]
    async fn malformed_host_response_fails_only_its_matching_dialog() {
        let app = AppHandle::new(std::io::sink());
        let first = app.dialog().file().pick_file();
        let second = app.dialog().file().pick_folder();
        tokio::pin!(first, second);
        assert!(futures_util::poll!(&mut first).is_pending());
        assert!(futures_util::poll!(&mut second).is_pending());
        assert!(app.resolve_dialog(&json!({"id":99,"result":null})).is_err());
        app.resolve_dialog(&json!({"id":2,"result":42})).unwrap();
        assert_eq!(
            second.await.unwrap_err(),
            "Dialog result must be a string or null"
        );
        assert!(futures_util::poll!(&mut first).is_pending());
        app.resolve_dialog(&json!({"id":1,"result":"/tmp/file"}))
            .unwrap();
        assert_eq!(first.await.unwrap(), Some(PathBuf::from("/tmp/file")));
    }
}
