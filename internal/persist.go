package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// pollVolumeFilename is fixed, not configurable — POLL_VOLUME points at a
// directory (the mount root), not a specific file, so the admin deploying
// this only ever needs to point at the volume itself, not also invent a
// filename within it.
const pollVolumeFilename = "poll.json"

// validatePollVolume confirms the configured volume is actually usable —
// exists, is a directory, and is genuinely writable — failing loudly at
// boot rather than silently falling back to ephemeral behavior. This is
// deliberately strict: POLL_VOLUME being set is a promise that a mounted,
// writable volume is there, and a promise that turns out to be false is
// exactly the kind of thing that should stop the process immediately, not
// be discovered only when a save silently doesn't stick.
//
// A real write, not just a permission-bit check: read-only mounts and
// similar don't always surface as a permission error on stat alone.
func validatePollVolume(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("POLL_VOLUME %q is not accessible: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("POLL_VOLUME %q is not a directory", dir)
	}

	probe := filepath.Join(dir, ".pollinator-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("POLL_VOLUME %q is not writable: %w", dir, err)
	}
	_ = os.Remove(probe)
	return nil
}

// loadPersistedPoll reads a previously-saved poll from the volume, if one
// exists. A missing file is not an error — it just means nothing has been
// saved there yet (a fresh volume, or first boot).
func loadPersistedPoll(dir string) (*Poll, error) {
	path := filepath.Join(dir, pollVolumeFilename)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading persisted poll: %w", err)
	}

	var p Poll
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("persisted poll at %s is corrupt: %w", path, err)
	}
	return &p, nil
}

// savePersistedPoll writes the current poll to the volume, overwriting
// whatever was there before. Called synchronously whenever SetPoll
// succeeds (see hub.go) — before the admin's save request gets its reply,
// not in the background afterward — so "saved" in the admin UI genuinely
// means durably saved, not just updated in memory with a write still
// pending. Write-to-temp-then-rename, not a direct write, so a crash or
// forced shutdown mid-write can never leave a half-written, corrupt
// poll.json behind — rename is atomic, so the file on disk is always
// either the complete old content or the complete new content.
func savePersistedPoll(dir string, poll *Poll) error {
	data, err := json.Marshal(poll)
	if err != nil {
		return err
	}

	final := filepath.Join(dir, pollVolumeFilename)
	tmp := final + ".tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing persisted poll: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("finalizing persisted poll: %w", err)
	}
	return nil
}
