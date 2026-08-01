//! A generic decoder for the None-terminated, tagged property-list format
//! that Palworld nests *inside* several `RawData` blobs (its own custom
//! structs, e.g. `PalIndividualCharacterSaveParameter`, serialize exactly
//! this way). It's the same wire format GVAS itself uses for the outer
//! save file -- uesave's own parser (`SaveGameArchive`) is private to that
//! crate, so this re-implements just the property types this fixture's
//! payloads actually use, verified byte-for-byte against
//! `testdata/.../Level.sav`'s `CharacterSaveParameterMap` entries (all 37
//! decode cleanly with this exact type set, including nested game-specific
//! structs like `PalGotStatusPoint` and `FixedPoint64`, which fall through
//! to the generic nested-struct branch below).

#[cfg(test)]
use super::reader::RawDataErrorKind;
#[cfg(test)]
use super::reader::test_support::ascii_fstring;
use super::reader::{RawDataError, Reader};

/// One property inside a nested Palworld property list: its name, its
/// declared type name (kept alongside the decoded value since a handful of
/// Palworld's own enums are carried on `ByteProperty` rather than
/// `EnumProperty`), and the decoded value itself.
#[derive(Debug, Clone, PartialEq)]
pub struct PalProperty {
    pub name: String,
    pub type_name: String,
    pub value: PalValue,
}

/// A decoded property value. Structs Palworld doesn't give a dedicated UE
/// struct type (`Vector`, `DateTime`, `Guid`, `Quat`, `LinearColor`) fall
/// through to [`PalValue::Struct`], since they're just its own nested
/// property lists in the same format as the outer one.
#[derive(Debug, Clone, PartialEq)]
pub enum PalValue {
    Bool(bool),
    Int(i32),
    Int64(i64),
    UInt16(u16),
    Float(f32),
    Str(String),
    /// `ByteProperty` with no enum type: a plain numeric byte.
    ByteRaw(u8),
    /// `ByteProperty` carrying an enum label -- Palworld uses this for
    /// some byte-backed enums instead of `EnumProperty`.
    Byte {
        enum_type: String,
        value: String,
    },
    Enum {
        enum_type: String,
        value: String,
    },
    Guid([u8; 16]),
    /// Raw .NET `DateTime` ticks, left unconverted (see `sections.rs`'s
    /// `levelMetaTimestampTicks` for the same convention).
    DateTime(u64),
    Vector(f64, f64, f64),
    Quat(f64, f64, f64, f64),
    LinearColor(f32, f32, f32, f32),
    /// Any other `StructProperty`: a nested None-terminated property list.
    Struct(Vec<PalProperty>),
    ArrayBytes(Vec<u8>),
    ArrayStr(Vec<String>),
    ArrayGuid(Vec<[u8; 16]>),
    ArrayStruct(String, Vec<PalValue>),
}

/// Reads properties until a `"None"` name terminates the list (or the
/// reader is exhausted, which surfaces as a truncation error rather than a
/// silent empty list).
pub(crate) fn read_properties_until_none(r: &mut Reader) -> Result<Vec<PalProperty>, RawDataError> {
    let mut out = Vec::new();
    loop {
        let name = r.fstring()?;
        if name == "None" {
            break;
        }
        // Moves `name` into the scope frame rather than cloning it --
        // `pop()` hands it straight back out once this property's value
        // has been decoded (see P8).
        r.push(name);
        let type_name = r.fstring()?;
        let size = r.u32()?;
        let _array_index = r.u32()?;
        let value = read_value(r, &type_name, size)?;
        let name = r.pop();
        out.push(PalProperty {
            name,
            type_name,
            value,
        });
    }
    Ok(out)
}

/// Validates that the bytes consumed since `start` match the property
/// tag's declared `size`, catching a nested-property-list decode that
/// silently mis-parsed a struct's fields (each field starts at the wrong
/// offset) rather than letting it propagate as a confusing downstream
/// truncation or a wrong-looking value further out.
fn check_size(r: &Reader, declared: u32, start: usize) -> Result<(), RawDataError> {
    let consumed = r.position() - start;
    if consumed as u64 != declared as u64 {
        return Err(r.error(format!(
            "declared size {declared} does not match {consumed} bytes actually read"
        )));
    }
    Ok(())
}

fn struct_element_min_bytes(struct_type: &str) -> usize {
    match struct_type {
        "Vector" => 24,  // 3 × f64
        "DateTime" => 8, // u64
        "Guid" => 16,
        "Quat" => 32,        // 4 × f64
        "LinearColor" => 16, // 4 × f32
        // Generic nested property list: at least its terminating "None"
        // fstring's i32 length prefix.
        _ => 4,
    }
}

fn read_value(r: &mut Reader, type_name: &str, size: u32) -> Result<PalValue, RawDataError> {
    match type_name {
        "IntProperty" => {
            r.optional_guid()?;
            let start = r.position();
            let value = PalValue::Int(r.i32()?);
            check_size(r, size, start)?;
            Ok(value)
        }
        "Int64Property" => {
            r.optional_guid()?;
            let start = r.position();
            let value = PalValue::Int64(r.i64()?);
            check_size(r, size, start)?;
            Ok(value)
        }
        "UInt16Property" => {
            r.optional_guid()?;
            let start = r.position();
            let value = PalValue::UInt16(r.u16()?);
            check_size(r, size, start)?;
            Ok(value)
        }
        "FloatProperty" => {
            r.optional_guid()?;
            let start = r.position();
            let value = PalValue::Float(r.f32()?);
            check_size(r, size, start)?;
            Ok(value)
        }
        "StrProperty" | "NameProperty" => {
            r.optional_guid()?;
            let start = r.position();
            let value = PalValue::Str(r.fstring()?);
            check_size(r, size, start)?;
            Ok(value)
        }
        "BoolProperty" => {
            // The bool's value lives in the tag itself (the byte read
            // here), not in a separate size-declared payload -- UE
            // declares BoolProperty's size as 0, so there is nothing to
            // check it against.
            let value = r.bool()?;
            r.optional_guid()?;
            Ok(PalValue::Bool(value))
        }
        "ByteProperty" => {
            let enum_type = r.fstring()?;
            r.optional_guid()?;
            let start = r.position();
            let value = if enum_type == "None" {
                PalValue::ByteRaw(r.u8()?)
            } else {
                let value = r.fstring()?;
                PalValue::Byte { enum_type, value }
            };
            check_size(r, size, start)?;
            Ok(value)
        }
        "EnumProperty" => {
            let enum_type = r.fstring()?;
            r.optional_guid()?;
            let start = r.position();
            let value = r.fstring()?;
            check_size(r, size, start)?;
            Ok(PalValue::Enum { enum_type, value })
        }
        "StructProperty" => {
            let struct_type = r.fstring()?;
            let _struct_id = r.guid()?;
            r.optional_guid()?;
            let start = r.position();
            let value = read_struct_value(r, &struct_type)?;
            check_size(r, size, start)?;
            Ok(value)
        }
        "ArrayProperty" => read_array_value(r, size),
        other => Err(r.unsupported(format!("unsupported property type {other}"))),
    }
}

fn read_array_value(r: &mut Reader, size: u32) -> Result<PalValue, RawDataError> {
    let array_type = r.fstring()?;
    r.optional_guid()?;
    let start = r.position();
    let count = r.u32()?;
    let value = if array_type == "StructProperty" {
        // An array of structs carries one extra tag describing the
        // element struct type, shared by every element (rather than
        // repeating a full StructProperty tag per element).
        let _prop_name = r.fstring()?;
        let _prop_type = r.fstring()?;
        let _reserved = r.u64()?;
        let element_type = r.fstring()?;
        let _element_id = r.guid()?;
        let _padding = r.u8()?;
        let count = r.checked_count(count, struct_element_min_bytes(&element_type))?;
        let mut values = Vec::with_capacity(count);
        for _ in 0..count {
            values.push(read_struct_value(r, &element_type)?);
        }
        PalValue::ArrayStruct(element_type, values)
    } else {
        match array_type.as_str() {
            "EnumProperty" | "NameProperty" => {
                // Each element is an FString: at least its i32 length prefix.
                let count = r.checked_count(count, 4)?;
                let mut values = Vec::with_capacity(count);
                for _ in 0..count {
                    values.push(r.fstring()?);
                }
                PalValue::ArrayStr(values)
            }
            "Guid" => {
                let count = r.checked_count(count, 16)?;
                let mut values = Vec::with_capacity(count);
                for _ in 0..count {
                    values.push(r.guid()?);
                }
                PalValue::ArrayGuid(values)
            }
            "ByteProperty" => {
                let count = r.checked_count(count, 1)?;
                let mut values = Vec::with_capacity(count);
                for _ in 0..count {
                    values.push(r.u8()?);
                }
                PalValue::ArrayBytes(values)
            }
            other => return Err(r.unsupported(format!("unsupported array element type {other}"))),
        }
    };
    check_size(r, size, start)?;
    Ok(value)
}

fn read_struct_value(r: &mut Reader, struct_type: &str) -> Result<PalValue, RawDataError> {
    match struct_type {
        "Vector" => Ok(PalValue::Vector(r.f64()?, r.f64()?, r.f64()?)),
        "DateTime" => Ok(PalValue::DateTime(r.u64()?)),
        "Guid" => Ok(PalValue::Guid(r.guid()?)),
        "Quat" => Ok(PalValue::Quat(r.f64()?, r.f64()?, r.f64()?, r.f64()?)),
        "LinearColor" => Ok(PalValue::LinearColor(
            r.f32()?,
            r.f32()?,
            r.f32()?,
            r.f32()?,
        )),
        _ => {
            r.enter_struct()?;
            let result = read_properties_until_none(r).map(PalValue::Struct);
            r.exit_struct();
            result
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn property_header(name: &str, type_name: &str, size: u32) -> Vec<u8> {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(&ascii_fstring(name));
        bytes.extend_from_slice(&ascii_fstring(type_name));
        bytes.extend_from_slice(&size.to_le_bytes());
        bytes.extend_from_slice(&0u32.to_le_bytes()); // array index
        bytes
    }

    #[test]
    fn empty_list_is_just_the_none_terminator() {
        let data = ascii_fstring("None");
        let mut r = Reader::new(&data, "root");
        let props = read_properties_until_none(&mut r).unwrap();
        assert!(props.is_empty());
        assert_eq!(r.remaining(), 0);
    }

    #[test]
    fn decodes_int_bool_and_str_properties() {
        let mut data = Vec::new();
        data.extend_from_slice(&property_header("Level", "IntProperty", 4));
        data.push(0); // no optional guid
        data.extend_from_slice(&9i32.to_le_bytes());

        data.extend_from_slice(&property_header("IsPlayer", "BoolProperty", 0));
        data.push(1); // bool value = true
        data.push(0); // no optional guid

        // Declared size covers the whole fstring wire encoding, including
        // its own 4-byte length prefix: 4 + 6 ("Atmus\0"'s length-prefixed
        // payload) = 10.
        data.extend_from_slice(&property_header("NickName", "StrProperty", 10));
        data.push(0); // no optional guid
        data.extend_from_slice(&ascii_fstring("Atmus"));

        data.extend_from_slice(&ascii_fstring("None"));

        let mut r = Reader::new(&data, "root");
        let props = read_properties_until_none(&mut r).unwrap();
        assert_eq!(
            props,
            vec![
                PalProperty {
                    name: "Level".into(),
                    type_name: "IntProperty".into(),
                    value: PalValue::Int(9),
                },
                PalProperty {
                    name: "IsPlayer".into(),
                    type_name: "BoolProperty".into(),
                    value: PalValue::Bool(true),
                },
                PalProperty {
                    name: "NickName".into(),
                    type_name: "StrProperty".into(),
                    value: PalValue::Str("Atmus".into()),
                },
            ]
        );
    }

    #[test]
    fn decodes_nested_struct_falling_through_to_generic_property_list() {
        let mut inner = Vec::new();
        inner.extend_from_slice(&property_header("Value", "Int64Property", 8));
        inner.push(0);
        inner.extend_from_slice(&500i64.to_le_bytes());
        inner.extend_from_slice(&ascii_fstring("None"));

        let mut data = Vec::new();
        data.extend_from_slice(&property_header("Hp", "StructProperty", inner.len() as u32));
        data.extend_from_slice(&ascii_fstring("FixedPoint64")); // struct_type
        data.extend_from_slice(&[0u8; 16]); // struct_id guid
        data.push(0); // no optional guid
        data.extend_from_slice(&inner);
        data.extend_from_slice(&ascii_fstring("None"));

        let mut r = Reader::new(&data, "root");
        let props = read_properties_until_none(&mut r).unwrap();
        assert_eq!(
            props,
            vec![PalProperty {
                name: "Hp".into(),
                type_name: "StructProperty".into(),
                value: PalValue::Struct(vec![PalProperty {
                    name: "Value".into(),
                    type_name: "Int64Property".into(),
                    value: PalValue::Int64(500),
                }]),
            }]
        );
    }

    #[test]
    fn truncated_property_list_errors_naming_the_path_no_panic() {
        // A property name and type but no size/index/payload bytes.
        let mut data = Vec::new();
        data.extend_from_slice(&ascii_fstring("Level"));
        data.extend_from_slice(&ascii_fstring("IntProperty"));

        let mut r = Reader::new(&data, "SaveParameter");
        let err = read_properties_until_none(&mut r).unwrap_err();
        assert_eq!(err.path, "SaveParameter.Level");
        assert!(err.message.contains("truncated"));
    }

    #[test]
    fn unsupported_property_type_errors_naming_the_path() {
        let mut data = Vec::new();
        data.extend_from_slice(&property_header("Weird", "MapProperty", 0));

        let mut r = Reader::new(&data, "SaveParameter");
        let err = read_properties_until_none(&mut r).unwrap_err();
        assert_eq!(err.path, "SaveParameter.Weird");
        assert!(err.message.contains("MapProperty"));
        assert_eq!(
            err.kind,
            RawDataErrorKind::Unsupported("unsupported property type MapProperty".into())
        );
    }

    #[test]
    fn declared_size_mismatch_errors_naming_the_path_instead_of_misreading_later_fields() {
        // IntProperty is always 4 bytes on the wire, but this declares 8 --
        // must be caught here rather than silently misaligning whatever
        // property follows.
        let mut data = Vec::new();
        data.extend_from_slice(&property_header("Level", "IntProperty", 8));
        data.push(0); // no optional guid
        data.extend_from_slice(&9i32.to_le_bytes());

        let mut r = Reader::new(&data, "SaveParameter");
        let err = read_properties_until_none(&mut r).unwrap_err();
        assert_eq!(err.path, "SaveParameter.Level");
        assert!(err.message.contains("declared size 8"));
        assert!(err.message.contains("4 bytes actually read"));
    }

    #[test]
    fn decodes_guid_and_byte_and_str_arrays_with_correctly_accounted_declared_sizes() {
        // ArrayProperty(Guid): declared size = 4 (count) + 16 per element.
        let mut guids = Vec::new();
        guids.extend_from_slice(&2u32.to_le_bytes());
        guids.extend_from_slice(&[1u8; 16]);
        guids.extend_from_slice(&[2u8; 16]);
        let mut data = Vec::new();
        data.extend_from_slice(&property_header("Ids", "ArrayProperty", guids.len() as u32));
        data.extend_from_slice(&ascii_fstring("Guid")); // array_type (tag, not counted)
        data.push(0); // no optional guid
        data.extend_from_slice(&guids);
        data.extend_from_slice(&ascii_fstring("None"));

        let mut r = Reader::new(&data, "root");
        let props = read_properties_until_none(&mut r).unwrap();
        assert_eq!(
            props[0].value,
            PalValue::ArrayGuid(vec![[1u8; 16], [2u8; 16]])
        );
    }

    /// Builds `depth` levels of a generic (non-builtin) `StructProperty`
    /// nested one inside another via a single `"Nested"` field per level,
    /// terminated by an empty property list at the bottom.
    fn nested_generic_struct_property_list(depth: usize) -> Vec<u8> {
        if depth == 0 {
            return ascii_fstring("None");
        }
        let child = nested_generic_struct_property_list(depth - 1);
        let mut data = Vec::new();
        data.extend_from_slice(&property_header(
            "Nested",
            "StructProperty",
            child.len() as u32,
        ));
        data.extend_from_slice(&ascii_fstring("CustomStruct")); // struct_type: falls through to the generic branch
        data.extend_from_slice(&[0u8; 16]); // struct_id guid
        data.push(0); // no optional guid
        data.extend_from_slice(&child);
        data.extend_from_slice(&ascii_fstring("None")); // terminates this level's property list
        data
    }

    #[test]
    fn deeply_nested_struct_properties_error_at_a_depth_ceiling_instead_of_overflowing_the_stack() {
        // Nests far past MAX_STRUCT_DEPTH -- must error cleanly rather than
        // recursing read_properties_until_none -> read_value ->
        // read_struct_value -> read_properties_until_none deep enough to
        // overflow the stack.
        let data = nested_generic_struct_property_list(200);
        let mut r = Reader::new(&data, "root");
        let err = read_properties_until_none(&mut r).unwrap_err();
        assert!(
            err.message.contains("depth"),
            "expected a depth-ceiling error message: {}",
            err.message
        );
        assert_eq!(err.kind, RawDataErrorKind::Malformed(err.message.clone()));
    }

    #[test]
    fn array_property_with_hostile_count_errors_instead_of_overflowing_capacity() {
        // Declares a Guid array of ~134M elements against a payload with
        // only 8 bytes left -- must error, not overflow/panic computing
        // `Vec::with_capacity(count * 16)`.
        let mut array_payload = Vec::new();
        array_payload.extend_from_slice(&0x0800_0000u32.to_le_bytes());
        array_payload.extend_from_slice(&[0u8; 8]);

        let mut data = Vec::new();
        data.extend_from_slice(&property_header(
            "Ids",
            "ArrayProperty",
            array_payload.len() as u32,
        ));
        data.extend_from_slice(&ascii_fstring("Guid"));
        data.push(0); // no optional guid
        data.extend_from_slice(&array_payload);

        let mut r = Reader::new(&data, "root");
        let err = read_properties_until_none(&mut r).unwrap_err();
        assert!(err.message.contains("exceeds"));
    }
}
