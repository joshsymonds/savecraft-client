use jomini::text::{ObjectReader, ValueReader};
use jomini::Windows1252Encoding;

/// Find a top-level key in an ObjectReader and return its ValueReader.
pub fn find_field<'data, 'tokens>(
    reader: &ObjectReader<'data, 'tokens, Windows1252Encoding>,
    target: &str,
) -> Option<ValueReader<'data, 'tokens, Windows1252Encoding>> {
    for (key, _op, value) in reader.fields() {
        if key.read_str() == target {
            return Some(value);
        }
    }
    None
}

/// Find a numbered entry (e.g. `0={}`) inside an object.
pub fn find_entry<'data, 'tokens>(
    reader: &ObjectReader<'data, 'tokens, Windows1252Encoding>,
    id: &str,
) -> Option<ValueReader<'data, 'tokens, Windows1252Encoding>> {
    for (key, _op, value) in reader.fields() {
        if key.read_str() == id {
            return Some(value);
        }
    }
    None
}

/// Read all string values from an array-like object (e.g. `civics={"a" "b" "c"}`).
pub fn read_string_array(
    value: &ValueReader<'_, '_, Windows1252Encoding>,
) -> Vec<String> {
    let mut result = Vec::new();
    if let Ok(arr) = value.read_array() {
        for item in arr.values() {
            if let Ok(s) = item.read_str() {
                result.push(s.into_owned());
            }
        }
    }
    result
}

/// Read all repeated values for a key in an object (e.g. `ethic="a" ethic="b"`).
pub fn read_repeated_strings(
    reader: &ObjectReader<'_, '_, Windows1252Encoding>,
    target: &str,
) -> Vec<String> {
    let mut result = Vec::new();
    for (key, _op, value) in reader.fields() {
        if key.read_str() == target {
            if let Ok(s) = value.read_str() {
                result.push(s.into_owned());
            }
        }
    }
    result
}

/// Resolve a Clausewitz name block into a display string.
///
/// Stellaris names come in several forms:
///   1. Template: `name={ key="PLANET_NAME_FORMAT" variables={ { key="PARENT" value={ key="Sol" } } { key="NUMERAL" value={ key="III" literal=yes } } } }`
///      → substitutes variables into the key pattern. PLANET_NAME_FORMAT → "[PARENT] [NUMERAL]" → "Sol III"
///   2. Direct key: `name={ key="FUN2_PLANET_Jurg-Sahuul" }` → strip known prefixes, return the name part
///   3. Bare value: `name=yes` or `name="entity_string"` → no useful name
pub fn read_display_name(
    reader: &ObjectReader<'_, '_, Windows1252Encoding>,
    field: &str,
) -> Option<String> {
    let val = find_field(reader, field)?;

    // Try to read as an object (cases 1 & 2)
    if let Ok(name_obj) = val.read_object() {
        let key = read_string(&name_obj, "key")?;

        // Collect variables if present
        let mut vars = std::collections::HashMap::new();
        if let Some(vars_val) = find_field(&name_obj, "variables") {
            if let Ok(vars_arr) = vars_val.read_array() {
                for item in vars_arr.values() {
                    if let Ok(var_obj) = item.read_object() {
                        if let (Some(k), Some(v_val)) =
                            (read_string(&var_obj, "key"), find_field(&var_obj, "value"))
                        {
                            // value is either { key="X" } or a plain string
                            if let Ok(v_obj) = v_val.read_object() {
                                if let Some(v_str) = read_string(&v_obj, "key") {
                                    vars.insert(k, v_str);
                                }
                            }
                        }
                    }
                }
            }
        }

        // Case 1: template with variables
        if !vars.is_empty() {
            if key.contains("NAME_FORMAT") || key.contains("_FORMAT") {
                // Common Stellaris pattern: PARENT + NUMERAL
                let parent = vars.get("PARENT").or_else(|| vars.get("NAME"));
                let numeral = vars.get("NUMERAL");
                match (parent, numeral) {
                    (Some(p), Some(n)) => return Some(format!("{} {}", p, n)),
                    (Some(p), None) => return Some(p.clone()),
                    _ => {
                        // Unknown variable pattern — join all values
                        let parts: Vec<&str> = vars.values().map(|s| s.as_str()).collect();
                        if !parts.is_empty() {
                            return Some(parts.join(" "));
                        }
                    }
                }
            }
            // Has variables but not a FORMAT key — try PARENT/NAME
            if let Some(name) = vars.get("NAME").or_else(|| vars.get("PARENT")) {
                return Some(name.clone());
            }
        }

        // Case 2: direct key — strip common prefixes
        let name = key
            .strip_prefix("SPEC_")
            .or_else(|| key.strip_prefix("FUN2_PLANET_"))
            .or_else(|| key.strip_prefix("FUN_PLANET_"))
            .or_else(|| key.strip_prefix("PLANET_"))
            .unwrap_or(&key);

        // Don't return template keys as names
        if name.contains("_FORMAT") || name.contains("NAME_1_OF") {
            return None;
        }

        return Some(name.replace('_', " "));
    }

    None
}

/// Read a string field from an object, returning None if missing.
pub fn read_string(
    reader: &ObjectReader<'_, '_, Windows1252Encoding>,
    target: &str,
) -> Option<String> {
    find_field(reader, target).and_then(|v| v.read_str().ok().map(|s| s.into_owned()))
}

/// Read a float field from an object, returning None if missing.
pub fn read_f64(
    reader: &ObjectReader<'_, '_, Windows1252Encoding>,
    target: &str,
) -> Option<f64> {
    find_field(reader, target).and_then(|v| v.read_str().ok().and_then(|s| s.parse().ok()))
}

/// Read an integer field from an object, returning None if missing.
pub fn read_i64(
    reader: &ObjectReader<'_, '_, Windows1252Encoding>,
    target: &str,
) -> Option<i64> {
    find_field(reader, target).and_then(|v| v.read_str().ok().and_then(|s| s.parse().ok()))
}
