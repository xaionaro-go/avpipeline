# TODO: Fix ffstream output switch deadlock

## In Progress
- [ ] Test on real test phone (reproduce issue, apply fix, verify no issue)

## Done
- [x] Identified POSSIBLE root cause (packets between OutputSwitch and OutputSyncer block)
- [x] Implement POSSIBLE fix (SwitchFlagPassPreviousOutput + ClearPreviousValue)
- [x] Unit tests proving race condition fix:
  - TestSwitchFlagPassPreviousOutput: PASS with fix
  - TestSwitchFlagPassPreviousOutput_WithoutFix: PASS (detects deadlock without fix)
- [x] Integration test verifying StreamMux configuration:
  - TestOutputSwitchDeadlock_SplitAV_Configuration: PASS

## Status
- Root cause: **POSSIBLE** - unit tests prove the mechanism works
- Fix: **POSSIBLE** - automated tests pass, awaiting phone validation
- Tests: PASS
