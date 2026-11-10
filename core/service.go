package core

import (
	"context"
)

// Service is the boundary between the GUI and the network core. The GUI
// only depends on this interface, so the demo backend, the real Backend or
// a test double can replace it without touching a view.
type Service interface {
	Login(ctx context.Context, email, password string) (*Session, error)
	Logout() error
	// CurrentSession returns the session restored from disk at startup, or
	// nil when signed out.
	CurrentSession() *Session

	Search(ctx context.Context, opt SearchOptions) (*BookPage, error)
	Favorites(ctx context.Context, page, count int) (*BookPage, error)
	// FavoriteStatus reads the current account's state, including when a list
	// response omitted it. SetFavorite changes the account only after an action.
	FavoriteStatus(ctx context.Context, book Book) (bool, error)
	SetFavorite(ctx context.Context, id string, saved bool) error
	History(ctx context.Context, page, count int) (*BookPage, error)
	Recommended(ctx context.Context, page, count int) (*BookPage, error)
	Popular(ctx context.Context, page, count int) (*BookPage, error)

	Profile(ctx context.Context) (*Profile, error)
	Doctor(ctx context.Context) (*DoctorReport, error)

	// Download reports bytes written through progress and returns the
	// final path. The context cancels the transfer; a canceled or failed
	// transfer never damages an existing file.
	Download(ctx context.Context, book Book, dir string, progress func(done, total int64)) (string, error)

	// FetchImage downloads a cover image so the book detail view can show
	// it. It returns the encoded bytes (PNG, JPEG, GIF, WebP or BMP), still
	// catchable by ui.DecodeBitmap.
	FetchImage(ctx context.Context, url string) ([]byte, error)
}

// BookFetcher is an optional Service capability for refreshing metadata that
// list endpoints omit, such as covers in download history. Demo services can
// keep showing placeholders without providing a separate detail endpoint.
type BookFetcher interface {
	FetchBook(ctx context.Context, id, hash string) (*Book, error)
}
