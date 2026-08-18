use jomini::TextTape;

/// Parsed fields from the Stellaris save `meta` file.
pub struct Meta {
    pub version: Option<String>,
    pub name: Option<String>,
    pub date: Option<String>,
    pub required_dlcs: Vec<String>,
    pub meta_fleets: Option<i64>,
    pub meta_planets: Option<i64>,
}

/// Parse the `meta` file from a Stellaris save.
///
/// The meta file is a small Clausewitz-format text file with top-level
/// key=value pairs. We use jomini to parse it and extract the fields we need.
pub fn parse(data: &[u8]) -> Result<Meta, String> {
    let tape = TextTape::from_slice(data).map_err(|e| format!("jomini parse error: {e}"))?;
    let reader = tape.utf8_reader();

    let mut meta = Meta {
        version: None,
        name: None,
        date: None,
        required_dlcs: Vec::new(),
        meta_fleets: None,
        meta_planets: None,
    };

    for (key, _op, value) in reader.fields() {
        let key_str = key.read_str();
        match key_str.as_ref() {
            "version" => {
                if let Ok(v) = value.read_str() {
                    meta.version = Some(v.into_owned());
                }
            }
            "name" => {
                if let Ok(v) = value.read_str() {
                    meta.name = Some(v.into_owned());
                }
            }
            "date" => {
                if let Ok(v) = value.read_str() {
                    meta.date = Some(v.into_owned());
                }
            }
            "required_dlcs" => {
                if let Ok(arr) = value.read_array() {
                    for item in arr.values() {
                        if let Ok(s) = item.read_str() {
                            meta.required_dlcs.push(s.into_owned());
                        }
                    }
                }
            }
            "meta_fleets" => {
                if let Ok(v) = value.read_str() {
                    meta.meta_fleets = v.parse().ok();
                }
            }
            "meta_planets" => {
                if let Ok(v) = value.read_str() {
                    meta.meta_planets = v.parse().ok();
                }
            }
            _ => {}
        }
    }

    Ok(meta)
}

#[cfg(test)]
mod tests {
    use super::parse;

    // Stellaris writes its save text as UTF-8 (unlike EU4's Windows-1252).
    // Decoding it as 1252 turned a Portuguese empire name into
    // "ConsciÃªncia de VirÃ­dia" in production identity/summary output.
    #[test]
    fn meta_name_is_decoded_as_utf8() {
        let meta = parse(
            "version=\"Circinus v4.0.21\"\nname=\"Consciência de Virídia\"\ndate=\"2318.01.01\"\n"
                .as_bytes(),
        )
        .expect("parse meta");
        assert_eq!(meta.name.as_deref(), Some("Consciência de Virídia"));
        assert_eq!(meta.date.as_deref(), Some("2318.01.01"));
    }
}
