use thiserror::Error;

#[derive(Debug, Error)]
pub enum MultizenError {
    #[error("database error: {0}")]
    Db(#[from] rusqlite::Error),

    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    #[error("serde error: {0}")]
    Serde(#[from] serde_json::Error),

    #[error("profile not found: {0}")]
    NotFound(String),

    #[error("profile already exists: {0}")]
    AlreadyExists(String),

    #[error("config error: {0}")]
    Config(String),

    #[error("launch error: {0}")]
    Launch(String),

    #[error("cdp error: {0}")]
    Cdp(String),
}

pub type Result<T> = std::result::Result<T, MultizenError>;
