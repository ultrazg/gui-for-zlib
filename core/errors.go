package core

import (
	"errors"
	"fmt"
)

// Sentinel errors. The GUI and any future CLI classify failures with
// errors.Is, so every layer must wrap these instead of replacing them.
var (
	ErrLoginFailed     = errors.New("login failed")
	ErrNotLoggedIn     = errors.New("not logged in")
	ErrNoDomain        = errors.New("no healthy domain available")
	ErrEmptyQuery      = errors.New("empty query")
	ErrNoID            = errors.New("no book id")
	ErrInvalidProxy    = errors.New("invalid proxy")
	ErrParseFailed     = errors.New("parse failed")
	ErrSessionExpired  = errors.New("session expired")
	ErrPhraseMinWords  = errors.New("phrase search needs at least two words")
	ErrUnsupported     = errors.New("unsupported")
	ErrDownloadFailed  = errors.New("download failed")
	ErrBotProtection   = errors.New("bot protection")
	ErrRedirectLoop    = errors.New("redirect loop")
	ErrChallengeFailed = errors.New("challenge failed")

	// Kept for the GUI contract. ErrNotSignedIn is the same condition as
	// ErrNotLoggedIn under the name the skeleton used.
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNotSignedIn        = ErrNotLoggedIn
	ErrCanceled           = errors.New("canceled")
)

// BotProtectionError reports a DiamWall/bot-wall block before any content
// reached the caller.
type BotProtectionError struct {
	StatusCode int
	URL        string
}

func (e *BotProtectionError) Error() string {
	return fmt.Sprintf("%v: HTTP %d at %s", ErrBotProtection, e.StatusCode, e.URL)
}

func (e *BotProtectionError) Unwrap() error { return ErrBotProtection }

// HTTPStatusError reports a non-2xx response that was not an EAPI error
// envelope.
type HTTPStatusError struct {
	StatusCode int
	URL        string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %d from %s", e.StatusCode, e.URL)
}

// errDownloadStalled is the cancel cause used when no bytes arrive for
// downloadStallTimeout.
var errDownloadStalled = errors.New("download stalled")
