//! Thin Tauri IPC command wrappers.

use tauri_specta::{collect_commands, Builder};

#[tauri::command]
#[specta::specta]
pub fn greet(name: String) -> Result<String, String> {
    Ok(format!("Hello, {name}! You've been greeted from Rust!"))
}

/// Shared specta command registration for the Tauri host and bindings export test.
/// Does not start the app — host bootstrap lives in [`crate::run`].
pub fn specta_builder() -> Builder<tauri::Wry> {
    Builder::<tauri::Wry>::new().commands(collect_commands![greet])
}

#[cfg(test)]
mod tests {
    use super::specta_builder;
    use specta_typescript::Typescript;

    /// Escape hatch: regenerate `src/lib/bindings.ts` without launching the GUI.
    /// Prefer debug startup export via `just dev` day-to-day; use this for lint/build/CI.
    #[test]
    fn export_bindings() {
        specta_builder()
            .export(Typescript::default(), "../src/lib/bindings.ts")
            .expect("Failed to export typescript bindings");
    }
}
