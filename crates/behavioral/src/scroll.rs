//! Humanized scroll jitter. Splits a large wheel delta into smaller
//! uneven steps so wheel events don't arrive as one perfect chunk.

fn lcg(seed: u64) -> impl Iterator<Item = f64> {
    let mut state = seed.max(1);
    std::iter::from_fn(move || {
        state = state.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);
        Some((state >> 33) as f64 / (1u64 << 31) as f64)
    })
}

pub fn humanized_scroll_steps(delta_y: f64, seed: u64) -> Vec<f64> {
    // Step count scales with magnitude: 6-14 steps for typical scrolls.
    let magnitude = delta_y.abs();
    let n = (6 + (magnitude / 120.0) as usize).clamp(3, 14);
    let mut rng = lcg(seed);
    // Generate raw weights, normalize so they sum to delta_y.
    let weights: Vec<f64> = (0..n).map(|_| 0.5 + rng.next().unwrap()).collect();
    let total: f64 = weights.iter().sum();
    weights
        .iter()
        .map(|w| delta_y * (w / total))
        .collect()
}
