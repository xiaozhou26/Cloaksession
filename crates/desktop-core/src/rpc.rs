//! JSON-lines command dispatch and host dialog responses.

use crate::{commands, runtime::AppHandle, AppState};
use serde::{de::DeserializeOwned, Serialize};
use serde_json::Value;

fn argument<T: DeserializeOwned>(args: &Value, name: &str) -> Result<T, String> {
    serde_json::from_value(args.get(name).cloned().unwrap_or(Value::Null))
        .map_err(|error| format!("Invalid argument '{name}': {error}"))
}

fn encode<T: Serialize>(result: Result<T, String>) -> Result<Value, String> {
    serde_json::to_value(result?).map_err(|error| error.to_string())
}

pub async fn dispatch(
    app: &AppHandle,
    state: &AppState,
    command: &str,
    args: Value,
) -> Result<Value, String> {
    if !args.is_object() {
        return Err("args must be an object".into());
    }
    match command {
        "activity_recent" => {
            encode(commands::activity::activity_recent(state, argument(&args, "limit")?).await)
        }
        "proxy_detect_geo" => {
            encode(commands::proxy::proxy_detect_geo(state, argument(&args, "proxy")?).await)
        }
        "fingerprint_generate" => encode(
            commands::fingerprint::fingerprint_generate(state, argument(&args, "seed")?).await,
        ),
        "fingerprint_devices" => encode(commands::fingerprint::fingerprint_devices(state).await),
        "fingerprint_locales" => encode(commands::fingerprint::fingerprint_locales(state).await),
        "fingerprint_reconcile" => encode(
            commands::fingerprint::fingerprint_reconcile(
                state,
                argument(&args, "fingerprint")?,
                argument(&args, "patch")?,
            )
            .await,
        ),
        "fingerprint_locale_for_country" => encode(
            commands::fingerprint::fingerprint_locale_for_country(
                state,
                argument(&args, "country")?,
            )
            .await,
        ),
        "dialog_pick_browser_binary" => {
            encode(commands::dialog::dialog_pick_browser_binary(state, app.clone()).await)
        }
        "dialog_pick_directory" => {
            encode(commands::dialog::dialog_pick_directory(state, app.clone()).await)
        }
        "update_status" => encode(commands::update::update_status(state).await),
        "update_last_checked" => encode(commands::update::update_last_checked(state).await),
        "update_check" => encode(commands::update::update_check(app.clone(), state).await),
        "update_install" => encode(commands::update::update_install(app.clone(), state).await),
        "update_download" => encode(
            commands::update::update_download(app.clone(), state, argument(&args, "version")?)
                .await,
        ),
        "system_info" => encode(commands::system::system_info(state).await),
        "profiles_list" => encode(commands::profiles::profiles_list(state).await),
        "profiles_get" => {
            encode(commands::profiles::profiles_get(state, argument(&args, "id")?).await)
        }
        "profiles_create" => {
            encode(commands::profiles::profiles_create(state, argument(&args, "input")?).await)
        }
        "profiles_update" => encode(
            commands::profiles::profiles_update(
                state,
                argument(&args, "id")?,
                argument(&args, "patch")?,
            )
            .await,
        ),
        "profiles_delete" => {
            encode(commands::profiles::profiles_delete(state, argument(&args, "id")?).await)
        }
        "profiles_launch" => encode(
            commands::profiles::profiles_launch(state, app.clone(), argument(&args, "id")?).await,
        ),
        "profiles_close" => {
            encode(commands::profiles::profiles_close(state, argument(&args, "id")?).await)
        }
        "settings_get" => encode(commands::settings::settings_get(state).await),
        "settings_update" => {
            encode(commands::settings::settings_update(state, argument(&args, "patch")?).await)
        }
        "profiles_export_archive" => encode(
            commands::archive::profiles_export_archive(
                state,
                app.clone(),
                argument(&args, "id")?,
                argument(&args, "passphrase")?,
            )
            .await,
        ),
        "profiles_import_archive" => encode(
            commands::archive::profiles_import_archive(
                state,
                app.clone(),
                argument(&args, "passphrase")?,
            )
            .await,
        ),
        "extensions_list" => encode(
            commands::extensions::extensions_list(state, argument(&args, "profileId")?).await,
        ),
        "extensions_add_from_web_store" => encode(
            commands::extensions::extensions_add_from_web_store(
                state,
                argument(&args, "profileId")?,
                argument(&args, "urlOrId")?,
            )
            .await,
        ),
        "extensions_add_from_file" => encode(
            commands::extensions::extensions_add_from_file(
                state,
                app.clone(),
                argument(&args, "profileId")?,
            )
            .await,
        ),
        "extensions_add_from_folder" => encode(
            commands::extensions::extensions_add_from_folder(
                state,
                app.clone(),
                argument(&args, "profileId")?,
            )
            .await,
        ),
        "extensions_remove" => encode(
            commands::extensions::extensions_remove(
                state,
                argument(&args, "profileId")?,
                argument(&args, "extId")?,
            )
            .await,
        ),
        "extensions_toggle" => encode(
            commands::extensions::extensions_toggle(
                state,
                argument(&args, "profileId")?,
                argument(&args, "extId")?,
                argument(&args, "enabled")?,
            )
            .await,
        ),
        "extensions_store_entries" => {
            encode(commands::extensions::extensions_store_entries(state).await)
        }
        "extensions_prepare_from_web_store" => encode(
            commands::extensions::extensions_prepare_from_web_store(
                state,
                argument(&args, "urlOrId")?,
            )
            .await,
        ),
        "extensions_prepare_from_file" => {
            encode(commands::extensions::extensions_prepare_from_file(state, app.clone()).await)
        }
        "extensions_prepare_from_folder" => {
            encode(commands::extensions::extensions_prepare_from_folder(state, app.clone()).await)
        }
        "extensions_icon" => encode(
            commands::extensions::extensions_icon(
                state,
                argument(&args, "ext")?,
                argument(&args, "profileId")?,
            )
            .await,
        ),
        _ => Err(format!("Unknown command: {command}")),
    }
}

/// A dedicated reader keeps host dialog responses flowing while commands run.
/// Unlike Tokio stdin, this thread cannot hold runtime shutdown open.
pub async fn serve_stdio(app: AppHandle, state: std::sync::Arc<AppState>) -> Result<(), String> {
    use std::io::BufRead;
    let (sender, receiver) = tokio::sync::mpsc::channel(64);
    std::thread::Builder::new()
        .name("core-stdin".into())
        .spawn(move || {
            let stdin = std::io::stdin();
            for line in stdin.lock().lines() {
                let failed = line.is_err();
                if sender
                    .blocking_send(line.map_err(|error| error.to_string()))
                    .is_err()
                    || failed
                {
                    break;
                }
            }
        })
        .map_err(|error| error.to_string())?;
    serve(app, state, receiver).await
}

async fn serve(
    app: AppHandle,
    state: std::sync::Arc<AppState>,
    mut lines: tokio::sync::mpsc::Receiver<Result<String, String>>,
) -> Result<(), String> {
    use futures_util::FutureExt;
    use serde_json::json;
    let mut commands = tokio::task::JoinSet::new();
    app.emit("core:ready", Value::Null)?;
    let result = async {
        loop {
            tokio::select! {
                completed = commands.join_next(), if !commands.is_empty() => {
                    if let Some(Err(error)) = completed {
                        tracing::error!(%error, "RPC task failed");
                    }
                }
                line = lines.recv() => {
                    let Some(line) = line else { break };
                    let line = line?;
                    let message: Value = match serde_json::from_str(&line) {
                        Ok(message) => message,
                        Err(error) => {
                            app.reply(Value::Null, Err(format!("Invalid JSON: {error}")))?;
                            continue;
                        }
                    };
                    if !message.is_object() {
                        app.reply(Value::Null, Err("Request must be an object".into()))?;
                        continue;
                    }
                    // Host replies have no command, even when their id matches a command id.
                    if message.get("command").is_none() {
                        if let Err(error) = app.resolve_dialog(&message) {
                            tracing::warn!(%error, "Invalid host dialog response");
                        }
                        continue;
                    }
                    let id = message.get("id").cloned().unwrap_or(Value::Null);
                    if !id.is_number() {
                        app.reply(Value::Null, Err("Request id must be a number".into()))?;
                        continue;
                    }
                    let Some(command) = message.get("command").and_then(Value::as_str) else {
                        app.reply(id, Err("command must be a string".into()))?;
                        continue;
                    };
                    let args = message.get("args").cloned().unwrap_or(Value::Null);
                    if !args.is_object() {
                        app.reply(id, Err("args must be an object".into()))?;
                        continue;
                    }
                    if command == "shutdown" {
                        app.reply(id, Ok(json!(null)))?;
                        break;
                    }
                    let app = app.clone();
                    let state = state.clone();
                    let command = command.to_string();
                    commands.spawn(async move {
                        let result = std::panic::AssertUnwindSafe(dispatch(&app, &state, &command, args))
                            .catch_unwind().await.unwrap_or_else(|_| Err("Command panicked".into()));
                        if let Err(error) = app.reply(id, result) {
                            tracing::warn!(%error, "RPC reply failed");
                        }
                    });
                }
            }
        }
        Ok(())
    }.await;
    app.close_dialogs();
    commands.abort_all();
    result
}
