use jomini::TextTape;

/// Parsed fields from the Stellaris save `meta` file.
pub struct Meta {
    pub version: Option<String>,
    pub name: Option<String>,
    pub date: Option<String>,
    pub player: Option<String>,
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
    let reader = tape.windows1252_reader();

    let mut meta = Meta {
        version: None,
        name: None,
        date: None,
        player: None,
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
            "player" => {
                if let Ok(v) = value.read_str() {
                    meta.player = Some(v.into_owned());
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
