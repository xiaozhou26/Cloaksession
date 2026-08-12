use behavioral::keyboard::humanized_keystroke_delays;

#[test]
fn one_delay_per_char() {
    let d = humanized_keystroke_delays("hello", 1);
    assert_eq!(d.len(), 5, "one delay per character");
}

#[test]
fn delays_are_reasonable_human_range() {
    let d = humanized_keystroke_delays("the quick brown fox", 3);
    for ms in &d {
        assert!(*ms >= 40 && *ms <= 400, "delay {ms} ms should be in 40-400ms human range");
    }
}

#[test]
fn deterministic_for_same_seed() {
    assert_eq!(
        humanized_keystroke_delays("abc", 10),
        humanized_keystroke_delays("abc", 10)
    );
}

#[test]
fn space_and_punctuation_slow_down() {
    // Average delay for "a. b. c." should be >= average for "abcdefgh"
    // because spaces/punctuation add a small pause.
    let text_slow = "a. b. c.";
    let text_fast = "abcdefgh";
    let avg_slow: f64 =
        humanized_keystroke_delays(text_slow, 5).iter().map(|x| *x as f64).sum::<f64>()
        / text_slow.len() as f64;
    let avg_fast: f64 =
        humanized_keystroke_delays(text_fast, 5).iter().map(|x| *x as f64).sum::<f64>()
        / text_fast.len() as f64;
    assert!(avg_slow > avg_fast, "punctuation should slow down: slow={avg_slow} fast={avg_fast}");
}
