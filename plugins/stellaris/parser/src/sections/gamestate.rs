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
