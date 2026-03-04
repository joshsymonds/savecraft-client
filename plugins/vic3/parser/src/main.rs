mod sections;

use clausewitz_core::{envelope, ndjson};
use std::io::Read;

fn main() {
    let mut input = Vec::new();
    if let Err(e) = std::io::stdin().read_to_end(&mut input) {
        ndjson::emit_error("read_error", &format!("failed to read stdin: {e}"));
        std::process::exit(1);
    }

    ndjson::emit_status("Extracting save file...");

    let save = match envelope::extract(&input) {
        Ok(s) => s,
        Err(e) => {
            ndjson::emit_error("envelope_error", &e);
            std::process::exit(1);
        }
    };

    ndjson::emit_status("Parsing gamestate...");

    // TODO: Parse gamestate with jomini, walk the tree, extract sections.
    // For now, emit a placeholder to validate the pipeline.
    let _ = save;

    ndjson::emit_error("not_implemented", "Vic3 parser is not yet implemented");
    std::process::exit(1);
}
