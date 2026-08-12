use cdp_driver::a11y::trim_accessibility_tree;
use serde_json::json;

#[test]
fn drops_ignored_node() {
    let tree = json!({"role": {"value": "button"}, "ignored": true, "name": {"value": ""}, "childIds": []});
    let trimmed = trim_accessibility_tree(tree, 5000, 40);
    assert!(trimmed.is_null(), "ignored node with no children → null");
}

#[test]
fn keeps_named_button() {
    let tree = json!({"role": {"value": "button"}, "ignored": false, "name": {"value": "Submit"}, "childIds": []});
    let trimmed = trim_accessibility_tree(tree, 5000, 40);
    assert!(trimmed.is_object());
    assert_eq!(
        trimmed.get("role").and_then(|r| r.get("value")).and_then(|v| v.as_str()),
        Some("button")
    );
}

#[test]
fn respects_max_nodes() {
    // A deep tree should stop at max_nodes.
    let tree = json!({"role": {"value": "generic"}, "name": {"value": ""}, "childIds": []});
    let trimmed = trim_accessibility_tree(tree, 0, 40);
    assert!(trimmed.is_null(), "max_nodes=0 → null");
}
