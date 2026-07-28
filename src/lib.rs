mod commands;

#[cfg(debug_assertions)]
use specta_typescript::Typescript;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
	let builder = commands::specta_builder();

	// Idiomatic tauri-specta: export on every debug launch (e.g. `just dev`).
	#[cfg(debug_assertions)]
	builder
		.export(Typescript::default(), "ui/src/lib/bindings.ts")
		.expect("Failed to export typescript bindings");

	tauri::Builder::default()
		.plugin(tauri_plugin_opener::init())
		.invoke_handler(builder.invoke_handler())
		.setup(move |app| {
			builder.mount_events(app);
			Ok(())
		})
		.run(tauri::generate_context!())
		.expect("error while running tauri application");
}
