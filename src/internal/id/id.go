// Package id owns identity generation for server-minted identifiers
// (ADR-035). It is the single place where IDs are created: transport
// shells and extraction models never mint persistent IDs.
//
// Two schemes live here:
//
//   - Task (and other server-generated) IDs are time-ordered ULIDs in
//     Crockford base32: "task-" + 26 chars = 48-bit unix-millisecond
//     timestamp + 80 crypto-random bits. Unique across processes without
//     coordination, sortable by creation time.
//
//   - Entity IDs are content-addressed per ADR-035 decision 2:
//     "ent-" + base32(sha256(scheme | normalized-content | category |
//     tenant | namespace))[:26]. Same content in the same scope always
//     yields the same ID, so dedup becomes insert-or-ignore; the scheme
//     version is part of the hash input so normalization changes cannot
//     silently rewrite identity.
package id

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Crockford base32 alphabet (no I, L, O, U).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ID grammar from ADR-035 decision 4: <type>-<26 Crockford base32 chars>.
// Case-insensitive: Crockford base32 accepts any letter case on input
// (canonical form is uppercase).
var (
	idPattern = regexp.MustCompile(`^(?i)(task|ent|ep|job)-([0-9A-HJKMNP-TV-Z]{26})$`)
	// ErrBadID is returned by Inspect/Validate for strings outside the
	// ADR-035 grammar.
	ErrBadID = errors.New("id: not a valid ADR-035 identifier")
)

// contentScheme versions the content-addressing formula. Bump when the
// normalizer or the field layout changes; old rows keep their IDs.
const contentScheme = "ent-v1"

// NewTaskID returns a unique, time-ordered task ID ("task-" + ULID).
// Cross-process safe: uniqueness comes from 80 crypto-random bits plus a
// millisecond timestamp, not from process-local counters.
func NewTaskID() string {
	return "task-" + newULID()
}

// ContentEntityID returns the deterministic entity ID for content in a
// scope. category, tenant, and namespace participate in the hash; the
// single-tenant runtime passes "" / DefaultNamespace equivalents.
func ContentEntityID(category, content string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		contentScheme,
		normalizeContent(content),
		category,
		"", // tenant — ADR-030 row-level tenancy slot
		"default",
	}, "|")))
	return "ent-" + encodeCrockford(sum[:])[:26]
}

// Info describes a parsed ADR-035 identifier.
type Info struct {
	Kind string // "task", "ent", "ep", "job"
	Raw  string
	Time string // decoded creation instant for timestamped kinds; "" otherwise
}

// Inspect parses an ADR-035 identifier and reports its kind and, for
// time-ordered IDs, its embedded creation timestamp.
func Inspect(raw string) (Info, error) {
	m := idPattern.FindStringSubmatch(strings.ToUpper(raw))
	if m == nil {
		return Info{}, fmt.Errorf("%w: %q", ErrBadID, raw)
	}
	info := Info{Kind: strings.ToLower(m[1]), Raw: raw}
	if info.Kind == "ent" {
		return info, nil // content-addressed: no embedded clock
	}
	ms, err := decodeCrockfordTime(m[2])
	if err != nil {
		return Info{}, err
	}
	info.Time = time.UnixMilli(ms).UTC().Format(time.RFC3339)
	return info, nil
}

// Validate reports whether raw matches the ADR-035 grammar.
func Validate(raw string) bool {
	return idPattern.MatchString(strings.ToUpper(raw))
}

// normalizeContent is the version-locked text normalization used by
// content addressing: Unicode NFC, lowercase, whitespace runs collapsed
// to single spaces, trimmed. Changing it requires bumping contentScheme.
func normalizeContent(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	prevSpace := true // trims leading whitespace
	for _, r := range norm.NFC.String(content) {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(unicode.ToLower(r))
	}
	return strings.TrimRight(b.String(), " ")
}

// newULID returns a 26-char Crockford base32 ULID: 48-bit unix-ms + 80
// random bits.
func newULID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	if _, err := rand.Read(b[6:16]); err != nil {
		panic("id: crypto/rand unavailable: " + err.Error())
	}
	return encodeCrockford(b[:])
}

// encodeCrockford renders bytes as uppercase Crockford base32.
func encodeCrockford(src []byte) string {
	var sb strings.Builder
	bitBuf := uint64(0)
	bits := uint(0)
	for _, by := range src {
		bitBuf = bitBuf<<8 | uint64(by)
		bits += 8
		for bits >= 5 {
			sb.WriteByte(crockford[(bitBuf>>(bits-5))&0x1f])
			bits -= 5
		}
	}
	if bits > 0 {
		sb.WriteByte(crockford[(bitBuf<<(5-bits))&0x1f])
	}
	return sb.String()
}

// decodeCrockfordTime extracts the 48-bit millisecond timestamp that
// opens a ULID-shaped body.
func decodeCrockfordTime(body string) (int64, error) {
	if len(body) != 26 {
		return 0, fmt.Errorf("%w: body length %d", ErrBadID, len(body))
	}
	var ms uint64
	for _, r := range body[:10] { // first 10 chars = 50 bits; top 48 are the ms field
		idx := strings.IndexRune(crockford, r)
		if idx < 0 {
			return 0, fmt.Errorf("%w: char %q", ErrBadID, r)
		}
		ms = ms<<5 | uint64(idx)
	}
	return int64(ms >> 2), nil
}
