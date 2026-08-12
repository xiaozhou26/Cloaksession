//! Humanized mouse path generation. Pure computation — no IO, no CDP.
//! Produces a series of intermediate points from `from` to `to` via a
//! quadratic Bezier curve with a per-seed control-point offset, sampled
//! at decreasing intervals to mimic deceleration toward the target.

const SAMPLES: usize = 12;

/// Deterministic LCG seeded from `seed`. Avoids pulling in rand for the
/// hot path; behavioral tests use rand separately if needed.
fn lcg(seed: u64) -> impl Iterator<Item = f64> {
    let mut state = seed.max(1);
    std::iter::from_fn(move || {
        state = state.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);
        Some((state >> 33) as f64 / (1u64 << 31) as f64)
    })
}

pub fn humanized_path(from: (f64, f64), to: (f64, f64), seed: u64) -> Vec<(f64, f64)> {
    let (x0, y0) = from;
    let (x1, y1) = to;
    let mut rng = lcg(seed);
    // Control point: midpoint + perpendicular jitter, bounded so the curve
    // stays roughly between from and to (no wild detours).
    let mx = (x0 + x1) / 2.0;
    let my = (y0 + y1) / 2.0;
    let dx = x1 - x0;
    let dy = y1 - y0;
    let perp_x = -dy;
    let perp_y = dx;
    let jitter = (rng.next().unwrap() - 0.5) * 0.3; // ±15% of segment length
    let cx = mx + perp_x * jitter;
    let cy = my + perp_y * jitter;

    let mut out = Vec::with_capacity(SAMPLES);
    for i in 1..=SAMPLES {
        // Ease-out: sample more densely near the target.
        let t = (i as f64 / SAMPLES as f64).powf(0.7);
        let one_t = 1.0 - t;
        let px = one_t * one_t * x0 + 2.0 * one_t * t * cx + t * t * x1;
        let py = one_t * one_t * y0 + 2.0 * one_t * t * cy + t * t * y1;
        out.push((px, py));
    }
    // Force the final point to exactly the target (numerical safety).
    if let Some(last) = out.last_mut() {
        *last = (x1, y1);
    }
    out
}
