package core

import (
	"time"
)

// Book is one item from a list endpoint. The core fields hold the upstream
// values; the GUI-facing conveniences (Title/Author/Rating/Pages) are the
// same values in display form so views do not parse upstream strings.
type Book struct {
	ID          string
	Hash        string
	ISBN        string
	ISBN10      string
	ISBN13      string
	URL         string
	Cover       string
	Title       string
	Author      string
	Authors     []string
	Publisher   string
	Year        string
	Language    string
	Extension   string
	Size        string
	Rating      float64
	Quality     string
	Description string
	Categories  string
	Edition     string
	Pages       int
	DownloadURL string
	Date        string
}

// Key is the stable identity used by lists and downloads: id:hash.
func (b Book) Key() string { return b.ID + ":" + b.Hash }

// BookPage is one page of books, as the list endpoints return them.
// TotalPages is upstream's own count; Total is derived for the GUI's
// "共 N 本" label and is 0 when upstream omits pagination.
type BookPage struct {
	Books      []Book
	Page       int
	Total      int
	TotalPages int
}

// Session is the result of a successful login. Cookies stay inside the
// client: the GUI never sees the user key.
type Session struct {
	ID    string
	Email string
	Name  string
}

// Profile is the account view, including the daily download quota.
// DownloadsTotal/DownloadsLeft keep the GUI's original field names.
// QuotaKnown distinguishes "quota exhausted" from "upstream did not say".
type Profile struct {
	ID              string
	Email           string
	Name            string
	Downloads       int // downloaded today
	DownloadsTotal  int // daily allowance
	DownloadsLeft   int // remaining today
	Favorites       int
	DailyReset      string
	Premium         *bool
	Confirmed       *bool
	DonationsActive *bool
	DonationsExpire string
	PersonalDomains []string
	QuotaKnown      bool
}

// DomainStatus is one row of the doctor report.
type DomainStatus struct {
	Domain  string
	OK      bool
	Latency time.Duration
	Error   string
}

// DoctorReport is the result of a domain health check.
type DoctorReport struct {
	Checked time.Time
	Domains []DomainStatus
}
