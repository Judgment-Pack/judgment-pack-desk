//go:build !unix

package desk

import (
	"errors"
	"io/fs"
)

// custodyChecked is false where this build cannot inspect ownership.
//
// The store then refuses rather than proceeding: a custody claim that cannot
// be checked is a claim, and this package's whole argument is that the claims
// it makes are the ones it holds. See custody.go.
const custodyChecked = false

// openNoFollow is zero where the platform has no such flag. It costs nothing
// here: `custodyChecked` is false on the same platforms, so the store refuses
// before any of this is reached.
const openNoFollow = 0

// connectionRefused has no portable spelling off Unix; an unmatched transport
// failure falls through to the vocabulary's residual.
var connectionRefused = errors.New("connection refused")

func ownerOf(fs.FileInfo) (uint32, bool) { return 0, false }

var effectiveUser = func() uint32 { return 0 }
