//! Browser activity history tracking.

use std::collections::VecDeque;
use std::sync::{Arc, Mutex};

use serde::Serialize;
use tokio::sync::broadcast;
use uuid::Uuid;

const CAPACITY: usize = 500;

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ActivityEvent {
    pub id: String,
    pub timestamp: String,
    pub tool: String,
    pub profile_id: Option<String>,
    pub args: serde_json::Value,
    pub status: String,
    pub summary: Option<String>,
    pub duration_ms: Option<u64>,
}

pub struct ActivityLog {
    events: Arc<Mutex<VecDeque<ActivityEvent>>>,
    tx: broadcast::Sender<ActivityEvent>,
}

impl ActivityLog {
    pub fn new() -> Self {
        let (tx, _) = broadcast::channel(256);
        Self {
            events: Arc::new(Mutex::new(VecDeque::with_capacity(CAPACITY))),
            tx,
        }
    }

    pub fn start_call(
        &self,
        tool: &str,
        profile_id: Option<String>,
        args: serde_json::Value,
    ) -> String {
        let id = Uuid::new_v4().to_string();
        let event = ActivityEvent {
            id: id.clone(),
            timestamp: chrono::Utc::now().to_rfc3339(),
            tool: tool.to_string(),
            profile_id,
            args: sanitize_args(args),
            status: "pending".to_string(),
            summary: None,
            duration_ms: None,
        };
        self.push(event);
        id
    }

    pub async fn finish(
        &self,
        id: &str,
        status: &str,
        summary: Option<String>,
        duration_ms: Option<u64>,
    ) {
        let mut guard = self.events.lock().expect("activity log mutex poisoned");
        if let Some(e) = guard.iter_mut().find(|e| e.id == id) {
            e.status = status.to_string();
            e.summary = summary;
            e.duration_ms = duration_ms;
            let _ = self.tx.send(e.clone());
        }
    }

    pub async fn recent(&self, limit: usize) -> Vec<ActivityEvent> {
        let guard = self.events.lock().expect("activity log mutex poisoned");
        guard.iter().rev().take(limit).cloned().collect()
    }

    /// Subscribe to the broadcast stream of activity events.
    ///
    /// Each `start_call` (pending) and `finish` (completed) emits a clone of
    /// the `ActivityEvent` to every active receiver. Receivers that lag the
    /// broadcast buffer (256 slots) will observe `RecvError::Lagged` from
    /// `recv()` — callers should log and continue.
    ///
    /// Used by the Tauri shell (P4.5) to bridge activity events to the
    /// frontend via `app.emit("activity:event", event)`.
    pub fn subscribe(&self) -> broadcast::Receiver<ActivityEvent> {
        self.tx.subscribe()
    }

    fn push(&self, event: ActivityEvent) {
        let _ = self.tx.send(event.clone());
        let mut guard = self.events.lock().expect("activity log mutex poisoned");
        if guard.len() >= CAPACITY {
            guard.pop_front();
        }
        guard.push_back(event);
    }
}

impl Default for ActivityLog {
    fn default() -> Self {
        Self::new()
    }
}

pub fn sanitize_args(args: serde_json::Value) -> serde_json::Value {
    match args {
        serde_json::Value::Object(mut map) => {
            if let Some(v) = map.get_mut("text").and_then(|v| v.as_str().map(str::to_string)) {
                if v.len() > 80 {
                    let truncated: String = v.chars().take(77).collect();
                    map.insert(
                        "text".to_string(),
                        serde_json::Value::String(format!("{truncated}...")),
                    );
                }
            }
            if let Some(p) = map.get_mut("proxy").and_then(|v| v.as_object_mut()) {
                p.remove("username");
                p.remove("password");
            }
            if let Some(c) = map.get_mut("cookies").and_then(|v| v.as_array_mut()) {
                for cookie in c {
                    if let Some(co) = cookie.as_object_mut() {
                        if let Some(val) = co.get_mut("value") {
                            *val = serde_json::Value::String("[redacted]".into());
                        }
                    }
                }
            }
            // Recurse into nested
            let mut new_map = serde_json::Map::new();
            for (k, v) in map {
                new_map.insert(k, sanitize_args(v));
            }
            serde_json::Value::Object(new_map)
        }
        serde_json::Value::Array(a) => {
            serde_json::Value::Array(a.into_iter().map(sanitize_args).collect())
        }
        other => other,
    }
}

