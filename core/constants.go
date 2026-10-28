package core

import (
	"strings"
	"time"
)

// Extension values accepted by search filters. Upstream expects lowercase
// in the form body; callers use the display form.
const (
	ExtTXT  = "TXT"
	ExtPDF  = "PDF"
	ExtFB2  = "FB2"
	ExtEPUB = "EPUB"
	ExtLIT  = "LIT"
	ExtMOBI = "MOBI"
	ExtRTF  = "RTF"
	ExtDJV  = "DJV"
	ExtDJVU = "DJVU"
	ExtAZW  = "AZW"
	ExtAZW3 = "AZW3"
)

// Extensions is the full filter list.
var Extensions = []string{ExtTXT, ExtPDF, ExtFB2, ExtEPUB, ExtLIT, ExtMOBI, ExtRTF, ExtDJV, ExtDJVU, ExtAZW, ExtAZW3}

// OrderOption is the search ordering enum.
type OrderOption string

const (
	OrderPopular      OrderOption = "popular"
	OrderDateCreated  OrderOption = "date_created"
	OrderDateUpdated  OrderOption = "date_updated"
	OrderBestMatch    OrderOption = "bestmatch"
	OrderDate         OrderOption = "date"
	OrderTitleAsc     OrderOption = "titleA"
	OrderTitle        OrderOption = "title"
	OrderYear         OrderOption = "year"
	OrderFilesizeDesc OrderOption = "filesize"
	OrderFilesizeAsc  OrderOption = "filesizeA"
)

// Languages the search form offers.
var Languages = []string{
	"english", "chinese", "russian", "french", "german", "spanish",
	"japanese", "korean", "italian", "portuguese", "arabic", "dutch",
	"polish", "turkish", "hindi",
}

// Defaults shared by the client and the backend.
const (
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	defaultTimeout = 180 * time.Second

	maxRedirects           = 10
	maxSearchCount         = 50
	defaultSearchCount     = 10
	DefaultDownloadRetries = 3

	downloadBufferSize = 32 * 1024
	downloadDrainLimit = 64 * 1024

	// MaxEAPIResponseBytes caps a single JSON response. Covers a full page
	// of detailed book records with room to spare.
	maxEAPIResponseBytes = 32 << 20

	// maxCoverBytes caps a fetched cover image. Covers are small, so a
	// stray URL must not let a whole file stage in memory.
	maxCoverBytes = 20 << 20

	eapiProbePath = "/eapi/info/domains"
)

// Retry/backoff and stall limits are vars so tests can shorten them.
var (
	downloadRetryBase    = 1 * time.Second
	downloadStallTimeout = 2 * time.Minute
)

// normalizeEnum lowercases and trims a filter value; empty stays empty.
func normalizeEnum(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
