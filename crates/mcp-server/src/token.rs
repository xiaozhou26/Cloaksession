use subtle::ConstantTimeEq;

pub fn token_matches(provided: &str, expected: &str) -> bool {
    if provided.len() != expected.len() {
        // Still do a comparison to keep timing roughly constant.
        let _ = expected.as_bytes().ct_eq(expected.as_bytes());
        return false;
    }
    provided.as_bytes().ct_eq(expected.as_bytes()).into()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn matches_same() { assert!(token_matches("abc123", "abc123")); }
    #[test]
    fn rejects_diff() { assert!(!token_matches("abc123", "abc124")); }
    #[test]
    fn rejects_diff_len() { assert!(!token_matches("abc", "abc123")); }
    #[test]
    fn rejects_empty() { assert!(!token_matches("", "abc123")); }
}
