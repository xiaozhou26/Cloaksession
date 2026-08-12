//! Humanized keystroke timing. Pure computation. Returns per-character
//! inter-key delays in milliseconds, drawn from a seed-determined
//! distribution with extra pause on whitespace/punctuation.

fn lcg(seed: u64) -> impl Iterator<Item = f64> {
    let mut state = seed.max(1);
    std::iter::from_fn(move || {
        state = state.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);
        Some((state >> 33) as f64 / (1u64 << 31) as f64)
    })
}

/// Approximate normal sample via Irwin–Hall (sum of 3 uniforms → roughly
/// normal around the mean). Mean 110ms, std ~35ms, clamped to [40, 400].
fn normal_ms(u1: f64, u2: f64, u3: f64) -> u64 {
    let mean = 110.0_f64;
    let std = 35.0_f64;
    // Irwin–Hall n=3 has mean 1.5, variance 0.25 → std 0.5.
    let z = ((u1 + u2 + u3) - 1.5) / 0.5;
    let ms = mean + z * std;
    ms.clamp(40.0, 400.0) as u64
}

pub fn humanized_keystroke_delays(text: &str, seed: u64) -> Vec<u64> {
    let mut rng = lcg(seed);
    let mut out = Vec::with_capacity(text.len());
    for ch in text.chars() {
        let u1 = rng.next().unwrap();
        let u2 = rng.next().unwrap();
        let u3 = rng.next().unwrap();
        let base = normal_ms(u1, u2, u3);
        // Extra pause on whitespace and sentence-ending punctuation.
        let extra = if ch.is_whitespace() {
            60u64
        } else if matches!(ch, '.' | ',' | '!' | '?') {
            90u64
        } else {
            0u64
        };
        out.push(base + extra);
    }
    out
}
