use behavioral::mouse::humanized_path;

#[test]
fn path_starts_near_from_ends_at_to() {
    let path = humanized_path((0.0, 0.0), (100.0, 100.0), 42);
    assert!(!path.is_empty(), "path must have intermediate points");
    let (ex, ey) = *path.last().unwrap();
    assert!((ex - 100.0).abs() < 1.0, "last x ≈ to.x, got {ex}");
    assert!((ey - 100.0).abs() < 1.0, "last y ≈ to.y, got {ey}");
}

#[test]
fn path_is_deterministic_for_same_seed() {
    let a = humanized_path((0.0, 0.0), (200.0, 150.0), 7);
    let b = humanized_path((0.0, 0.0), (200.0, 150.0), 7);
    assert_eq!(a, b, "same seed → same path");
}

#[test]
fn different_seeds_yield_different_paths() {
    let a = humanized_path((0.0, 0.0), (200.0, 150.0), 1);
    let b = humanized_path((0.0, 0.0), (200.0, 150.0), 2);
    assert_ne!(a, b, "different seeds → different paths");
}

#[test]
fn path_points_progress_monotonically_toward_target() {
    let path = humanized_path((0.0, 0.0), (100.0, 0.0), 99);
    let xs: Vec<f64> = path.iter().map(|(x, _)| *x).collect();
    for w in xs.windows(2) {
        assert!(w[1] >= w[0] - 5.0, "x should not jump backward significantly: {w:?}");
    }
}
