use serde::Serialize;

/// App-wide error for domain logic and Tauri commands.
///
/// Commands return `Result<T, AppError>`; `Serialize` is required so errors
/// cross the IPC boundary. Prefer this over `anyhow`/`String` at the command edge.
#[derive(Debug, thiserror::Error, specta::Type)]
pub enum AppError {
    #[error("{0}")]
    NotFound(String),
    #[error("{0}")]
    BadRequest(String),
}

/// Serialized / specta shape of [`AppError`] (`{ kind, message }`).
#[derive(Serialize, specta::Type)]
#[serde(tag = "kind", content = "message")]
pub enum AppErrorKind {
    NotFound(String),
    BadRequest(String),
}

impl Serialize for AppError {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        let message = self.to_string();
        let kind = match self {
            Self::NotFound(_) => AppErrorKind::NotFound(message),
            Self::BadRequest(_) => AppErrorKind::BadRequest(message),
        };
        kind.serialize(serializer)
    }
}
