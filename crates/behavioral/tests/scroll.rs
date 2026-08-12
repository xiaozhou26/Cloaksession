use behavioral::scroll::humanized_scroll_steps;

#[test]
fn steps_sum_to_delta() {
    let delta = 600.0;
    let steps = humanized_scroll_steps(delta, 1);
    let sum: f64 = steps.iter().sum();
    assert!((sum - delta).abs() < 5.0, "steps should sum to delta, got {sum}");
}

#[test]
fn no_single_step_dominates() {
    let steps = humanized_scroll_steps(1000.0, 2);
    let max = steps.iter().cloned().fold(0.0_f64, f64::max);
    assert!(max < 400.0, "no single step should dominate: max={max}");
}

#[test]
fn deterministic() {
    assert_eq!(
        humanized_scroll_steps(300.0, 9),
        humanized_scroll_steps(300.0, 9)
    );
}
