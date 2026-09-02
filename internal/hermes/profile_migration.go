package hermes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

const maxLegacyOptOutMarkerBytes int64 = 256

var (
	// ErrBundledSkillsUserOptOut preserves a marker that Team Kit cannot prove
	// was written by the legacy Hermes --no-skills profile creation command.
	ErrBundledSkillsUserOptOut = errors.New("HERMES_BUNDLED_SKILLS_USER_OPT_OUT")
	// ErrBundledSkillsMigrationFailed reports a failed or unverifiable Hermes
	// opt-in migration.
	ErrBundledSkillsMigrationFailed = errors.New("HERMES_BUNDLED_SKILLS_MIGRATION_FAILED")
	// ErrForeignHermesProfile reports a profile root replacement discovered
	// while a path-based Hermes command was executing.
	ErrForeignHermesProfile = errors.New("FOREIGN_HERMES_PROFILE")
)

var legacyOptOutMarkerLF = []byte("This profile opted out of bundled-skill seeding (`hermes profile create --no-skills`).\nDelete this file to re-enable sync on the next `hermes update`.\n")
var legacyOptOutMarkerCRLF = []byte(strings.ReplaceAll(string(legacyOptOutMarkerLF), "\n", "\r\n"))

// OwnershipVerifier proves that profileRoot is the exact Team Kit-owned
// profile for identity. It must return FOREIGN_HERMES_PROFILE when that proof
// no longer holds.
type OwnershipVerifier func(profileRoot, identity string) error

type profileRootIdentity struct {
	Key string
}

type openedProfileRoot interface {
	Identity() profileRootIdentity
	ReadLegacyOptOutMarker() ([]byte, error)
	VerifyPath() error
	Close() error
}

// Test seam: production keeps this a no-op; tests exercise a root replacement
// after the marker read but before any success return.
var afterProfileMarkerRead = func() {}

// BundledSkillsOptIn is the narrow Hermes command used to restore bundled
// skills without recreating or otherwise modifying a profile.
type BundledSkillsOptIn interface {
	OptInBundledSkills(context.Context, string) error
}

// ExactLegacyOptOutMarker reports whether profileRoot has exactly the marker
// written by legacy Hermes profile creation. A missing marker is not an error.
func ExactLegacyOptOutMarker(profileRoot string) (bool, error) {
	root, err := openProfileRoot(profileRoot)
	if err != nil {
		return false, ErrBundledSkillsUserOptOut
	}
	defer root.Close()
	marker, err := exactLegacyOptOutMarker(root)
	if err != nil {
		return false, err
	}
	if err := root.VerifyPath(); err != nil {
		return false, ErrBundledSkillsUserOptOut
	}
	return marker, nil
}

func exactLegacyOptOutMarker(root openedProfileRoot) (bool, error) {
	data, err := root.ReadLegacyOptOutMarker()
	afterProfileMarkerRead()
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil || (!bytes.Equal(data, legacyOptOutMarkerLF) && !bytes.Equal(data, legacyOptOutMarkerCRLF)) {
		return false, ErrBundledSkillsUserOptOut
	}
	return true, nil
}

// MigrateOwnedBundledSkills restores bundled skills only when a legacy marker,
// profile ownership, and the profile root identity can all be proven stable.
func MigrateOwnedBundledSkills(ctx context.Context, profileRoot, identity string, verify OwnershipVerifier, cli BundledSkillsOptIn) error {
	if verify == nil {
		return ErrForeignHermesProfile
	}
	if cli == nil {
		return ErrBundledSkillsMigrationFailed
	}
	if err := verify(profileRoot, identity); err != nil {
		return err
	}
	root, err := openProfileRoot(profileRoot)
	if err != nil {
		return ErrForeignHermesProfile
	}
	defer root.Close()
	marker, err := exactLegacyOptOutMarker(root)
	if err != nil {
		return err
	}
	if !marker {
		if err := verify(profileRoot, identity); err != nil {
			return err
		}
		if err := root.VerifyPath(); err != nil {
			return ErrForeignHermesProfile
		}
		return nil
	}
	if err := verify(profileRoot, identity); err != nil {
		return err
	}
	if err := root.VerifyPath(); err != nil {
		return ErrForeignHermesProfile
	}
	commandErr := cli.OptInBundledSkills(ctx, identity)
	if err := verify(profileRoot, identity); err != nil {
		return err
	}
	if err := root.VerifyPath(); err != nil {
		return ErrForeignHermesProfile
	}
	if commandErr != nil {
		return fmt.Errorf("%w: opt-in command did not complete", ErrBundledSkillsMigrationFailed)
	}
	marker, err = exactLegacyOptOutMarker(root)
	if err != nil || marker {
		return ErrBundledSkillsMigrationFailed
	}
	if err := root.VerifyPath(); err != nil {
		return ErrForeignHermesProfile
	}
	return nil
}
