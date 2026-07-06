package fsutil

import "os"

// WriteOwnerOnly writes data to path with mode 0o600 AND issues an
// os.Chmod(path, 0o600). Plain os.WriteFile is NOT enough on its own:
// open(2)'s mode argument only applies on file CREATION, so a
// pre-existing 0o644 file (e.g. an upgraded-from-0.3.x install) would
// stay 0o644 after a WriteFile+truncate, silently downgrading the
// security posture on every mutation. The post-WriteFile Chmod is
// what actively narrows the legacy case.
//
// The helper is the single chokepoint for "write a file with strict
// owner-only mode obligations." Every caller MUST route through it
// rather than reaching for os.WriteFile directly. Bypassing requires
// an explicit security review.
//
// data is written verbatim — no encoding layer is applied; the bytes
// the caller passes are the bytes on disk.
//
// Partial-failure contract: if os.WriteFile succeeds but os.Chmod
// fails (rare on POSIX; possible on EROFS / EPERM / NFS-mounted
// shares with restrictive ACLs), the file is on disk at the
// pre-existing mode (0o644 for legacy installs) and the os.Chmod
// error propagates unchanged. The caller may retry; on retry, the
// new WriteFile preserves the pre-existing mode again but the
// post-WriteFile Chmod eventually narrows it. The helper's error
// return is the authoritative signal for retry.
//
// # MIGRATION TRIGGER
//
// This helper lives in core/fsutil because two internal/ packages
// already have package-local duplicates with the exact same body:
//
//   - src/internal/config/update.go: writeConfig(path, content) error
//   - src/internal/ingestion/checkpoint.go: writeOwnerOnly(path, data) error
//
// Each duplicate is intentional at 2 sites. Migrate when a third
// caller appears anywhere in the codebase — remove the package-local
// duplicates and route all three through fsutil.WriteOwnerOnly.
// (Rule of three: two near-identical implementations tolerated as
// parallel; three forces consolidation.) Until that third caller
// arrives, cross-package primitive exposure is premature. At that
// moment, perform a single consolidation PR:
//
//  1. Pick the third package (e.g. a hypothetical src/internal/store
//     needing `func writeStoreOnly(path string, data []byte) error`
//     with the same WriteFile+Chmod body) and GET REVIEW on whether
//     the duplicate SEMANTICS diverge from this helper before
//     migration.
//  2. In src/internal/{config,ingestion}/{update.go,checkpoint.go},
//     replace the package-local writeConfig/writeOwnerOnly function
//     blocks with a delegation to fsutil.WriteOwnerOnly. Delete the
//     local duplicates.
//  3. Add `import "github.com/pavelveter/hermem/src/internal/core/fsutil"`
//     to whichever new caller landed.//   4. Divergence rule: if the third caller needs different
//     semantics (different post-write narrow mode, different
//     error-channel wrap, atomic-rename sequencing, etc.), fork
//     the helper into a SIBLING rather than overloading
//     WriteOwnerOnly. Two concrete divergence surfaces already
//     exist in this codebase, each is its own future helper:
//     - WriteGroupOnly for 0o640 group-readable mode
//     - WritePendingTmp for the streaming + atomic-rename
//     variant (e.g. matching
//     src/internal/ingestion/checkpoint.go writePendingTmp).
//     Insisting on named siblings keeps the security contract
//     inspectable: any caller querying the helper name learns the
//     narrowing intent from the name alone.
//
// Until a third caller exists, the package-local duplicates are
// preferred over cross-package exposure — each package owns its
// write-chokepoint, and a future contributor adding a new
// writeOwnerOnly-like primitive in a third package should follow the
// exact same body verbatim until the migration moment arrives ("rule
// of three"). Premature consolidation would expose core/fsutil
// internals to packages that don't yet need them and inflate the
// external dependency surface.
func WriteOwnerOnly(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
