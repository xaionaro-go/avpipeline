# TODO: Fix ffstream output switch deadlock

## COMPLETED ✅

### Evidence
- **Test Phone (192.168.0.159)**: Output switch `1 -> 3` at [0015] completed successfully
  - No panic, no deadlock, clean exit
  - Process ran for 16 seconds with auto_bitrate enabled
  - `MuxModeDifferentOutputsSameTracksSplitAV` mode with `hevc_mediacodec` encoder
  
### Fixes Applied
1. **SwitchFlagPassPreviousOutput** (fa0738c): Allow barrier to pass packets for previous output value during transition
2. **ErrCopyEncoder check** (cfd7276): Return error instead of panic when SendInputFrame called on copy encoder

### Tests
- Unit tests: PASS
- Integration tests: PASS  
- Real phone test: PASS

## Done
- [x] Identified root cause (packets between OutputSwitch and OutputSyncer block)
- [x] Implement fix (SwitchFlagPassPreviousOutput + ClearPreviousValue)
- [x] Unit tests proving race condition fix
- [x] Integration test verifying StreamMux configuration
- [x] Build ffstream for Android (arm64)
- [x] Deploy to test phone
- [x] Fix EncoderCopy panic (copy encoder in SendInputFrame)
- [x] Reproduce output switch on test phone - **SUCCESS**

## Status
- Root cause: **CONFIRMED** - fixed and validated on test phone
- Fix: **CONFIRMED** - output switch 1->3 completed without hang
- Tests: ALL PASS
