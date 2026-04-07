use jomini::text::ObjectReader;
use jomini::Windows1252Encoding;
use serde::Serialize;
use std::collections::HashMap;

use super::gamestate::find_field;

/// The economy section: income, expenses, and net balance by resource.
#[derive(Debug, Serialize)]
pub struct Economy {
    /// Total income per resource (summed across all categories).
    pub income: HashMap<String, f64>,
    /// Total expenses per resource (summed across all categories, positive values).
    pub expenses: HashMap<String, f64>,
    /// Net balance per resource (income - expenses).
    pub net: HashMap<String, f64>,
    /// Income broken down by category → resource → amount.
    pub income_by_category: HashMap<String, HashMap<String, f64>>,
    /// Expenses broken down by category → resource → amount.
    pub expenses_by_category: HashMap<String, HashMap<String, f64>>,
}

/// Extract the economy section from the player's country object.
pub fn extract(country: &ObjectReader<'_, '_, Windows1252Encoding>) -> Economy {
    let mut economy = Economy {
        income: HashMap::new(),
        expenses: HashMap::new(),
        net: HashMap::new(),
        income_by_category: HashMap::new(),
        expenses_by_category: HashMap::new(),
    };

    let budget_val = match find_field(country, "budget") {
        Some(v) => v,
        None => return economy,
    };
    let budget = match budget_val.read_object() {
        Ok(o) => o,
        Err(_) => return economy,
    };

    let current_val = match find_field(&budget, "current_month") {
        Some(v) => v,
        None => return economy,
    };
    let current = match current_val.read_object() {
        Ok(o) => o,
        Err(_) => return economy,
    };

    // Parse income categories
    if let Some(income_val) = find_field(&current, "income") {
        if let Ok(income_obj) = income_val.read_object() {
            aggregate_categories(&income_obj, &mut economy.income, &mut economy.income_by_category);
        }
    }

    // Parse expense categories
    if let Some(expenses_val) = find_field(&current, "expenses") {
        if let Ok(expenses_obj) = expenses_val.read_object() {
            aggregate_categories(
                &expenses_obj,
                &mut economy.expenses,
                &mut economy.expenses_by_category,
            );
        }
    }

    // Make expenses positive (they come as positive from the save but represent outflows)
    // Compute net
    let all_resources: std::collections::HashSet<&String> =
        economy.income.keys().chain(economy.expenses.keys()).collect();
    for resource in all_resources {
        let inc = economy.income.get(resource).copied().unwrap_or(0.0);
        let exp = economy.expenses.get(resource).copied().unwrap_or(0.0);
        economy.net.insert(resource.clone(), inc - exp);
    }

    economy
}

/// Walk a category-grouped object (income or expenses) and sum by resource.
///
/// Structure: `{ category_name={ resource=amount resource=amount } category_name={...} }`
fn aggregate_categories(
    obj: &ObjectReader<'_, '_, Windows1252Encoding>,
    totals: &mut HashMap<String, f64>,
    by_category: &mut HashMap<String, HashMap<String, f64>>,
) {
    for (cat_key, _op, cat_val) in obj.fields() {
        let category = cat_key.read_str().into_owned();
        if let Ok(cat_obj) = cat_val.read_object() {
            let mut cat_resources = HashMap::new();
            for (res_key, _op2, res_val) in cat_obj.fields() {
                let resource = res_key.read_str().into_owned();
                if let Ok(val_str) = res_val.read_str() {
                    if let Ok(amount) = val_str.parse::<f64>() {
                        let abs_amount = amount.abs();
                        *totals.entry(resource.clone()).or_insert(0.0) += abs_amount;
                        cat_resources.insert(resource, abs_amount);
                    }
                }
            }
            if !cat_resources.is_empty() {
                by_category.insert(category, cat_resources);
            }
        }
    }
}
