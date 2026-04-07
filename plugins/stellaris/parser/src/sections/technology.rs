use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;
use std::collections::HashMap;

use super::gamestate::{find_field, read_string_array};

/// A tech currently being researched.
#[derive(Debug, Serialize)]
pub struct InProgressTech {
    pub tech: String,
    pub progress: f64,
}

/// The technology section: researched techs, in-progress research, alternatives.
#[derive(Debug, Serialize)]
pub struct Technology {
    /// All researched technology IDs.
    pub researched: Vec<String>,
    /// Researched tech count.
    pub researched_count: usize,
    /// Repeatable techs with their levels.
    pub repeatables: HashMap<String, i64>,
    /// In-progress research by category.
    pub in_progress: HashMap<String, InProgressTech>,
    /// Available alternative techs by category.
    pub alternatives: HashMap<String, Vec<String>>,
}

/// Extract the technology section from the player's country object.
pub fn extract(country: &ObjectReader<'_, '_, Windows1252Encoding>) -> Technology {
    let mut tech = Technology {
        researched: Vec::new(),
        researched_count: 0,
        repeatables: HashMap::new(),
        in_progress: HashMap::new(),
        alternatives: HashMap::new(),
    };

    let tech_status_val = match find_field(country, "tech_status") {
        Some(v) => v,
        None => return tech,
    };
    let tech_status = match tech_status_val.read_object() {
        Ok(o) => o,
        Err(_) => return tech,
    };

    // Extract researched techs (repeated technology="name" level=N pairs)
    let mut seen = std::collections::HashSet::new();
    let mut current_tech: Option<String> = None;
    for (key, _op, value) in tech_status.fields() {
        let key_str = key.read_str();
        match key_str.as_ref() {
            "technology" => {
                if let Ok(name) = value.read_str() {
                    current_tech = Some(name.into_owned());
                }
            }
            "level" => {
                if let (Some(tech_name), Ok(level_str)) = (current_tech.take(), value.read_str()) {
                    let level: i64 = level_str.parse().unwrap_or(1);
                    if tech_name.contains("repeatable") && level > 1 {
                        tech.repeatables.insert(tech_name.clone(), level);
                    }
                    if seen.insert(tech_name.clone()) {
                        tech.researched.push(tech_name);
                    }
                }
            }
            _ => {
                // We've moved past the technology/level pairs
                current_tech = None;
            }
        }
    }

    tech.researched.sort();
    tech.researched_count = tech.researched.len();

    // Extract in-progress research from queues
    for category in &["physics", "society", "engineering"] {
        let queue_key = format!("{category}_queue");
        if let Some(queue_val) = find_field(&tech_status, &queue_key) {
            if let Ok(queue_arr) = queue_val.read_array() {
                // First entry in the queue is the current research
                for item in queue_arr.values() {
                    if let Ok(item_obj) = item.read_object() {
                        let mut tech_name = None;
                        let mut progress = 0.0;
                        for (k, _op, v) in item_obj.fields() {
                            match k.read_str().as_ref() {
                                "technology" => {
                                    if let Ok(s) = v.read_str() {
                                        tech_name = Some(s.into_owned());
                                    }
                                }
                                "progress" => {
                                    if let Ok(s) = v.read_str() {
                                        progress = s.parse().unwrap_or(0.0);
                                    }
                                }
                                _ => {}
                            }
                        }
                        if let Some(name) = tech_name {
                            tech.in_progress.insert(
                                category.to_string(),
                                InProgressTech {
                                    tech: name,
                                    progress,
                                },
                            );
                        }
                    }
                    break; // Only first entry (current research)
                }
            }
        }
    }

    // Extract alternatives (available techs)
    if let Some(alt_val) = find_field(&tech_status, "alternatives") {
        if let Ok(alt_obj) = alt_val.read_object() {
            for category in &["physics", "society", "engineering"] {
                if let Some(cat_val) = find_field(&alt_obj, category) {
                    let techs = read_string_array(&cat_val);
                    if !techs.is_empty() {
                        tech.alternatives.insert(category.to_string(), techs);
                    }
                }
            }
        }
    }

    tech
}
