package core

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// SearchOptions carries a search request. The GUI's fields (Query,
// Extension, Language, Page, Count) map onto it; the slice forms are what
// the wire protocol actually sends.
type SearchOptions struct {
	Query     string
	Extension string // single-select convenience; merged into Extensions
	Language  string // single-select convenience; merged into Languages

	Extensions []string
	Languages  []string
	Exact      bool
	FromYear   int
	ToYear     int
	Order      OrderOption

	Page  int
	Count int
}

// searchEAPI performs POST /eapi/book/search.
func (c *Client) searchEAPI(ctx context.Context, opt SearchOptions) (*BookPage, error) {
	query := strings.TrimSpace(opt.Query)
	if query == "" {
		return nil, ErrEmptyQuery
	}
	page := opt.Page
	if page < 1 {
		page = 1
	}
	count := opt.Count
	if count < 1 {
		count = defaultSearchCount
	}
	if count > maxSearchCount {
		count = maxSearchCount
	}

	form := url.Values{}
	form.Set("message", query)
	form.Set("page", strconv.Itoa(page))
	form.Set("limit", strconv.Itoa(count))
	if opt.Exact {
		form.Set("e", "1")
	}
	if opt.FromYear > 0 {
		form.Set("yearFrom", strconv.Itoa(opt.FromYear))
	}
	if opt.ToYear > 0 {
		form.Set("yearTo", strconv.Itoa(opt.ToYear))
	}
	if opt.Order != "" {
		form.Set("order", string(opt.Order))
	}
	for _, lang := range mergeFilter(opt.Languages, opt.Language) {
		form.Add("languages[]", normalizeEnum(lang))
	}
	for _, ext := range mergeFilter(opt.Extensions, opt.Extension) {
		form.Add("extensions[]", normalizeEnum(ext))
	}

	body, err := c.eapiRequest(ctx, http.MethodPost, "/eapi/book/search", form)
	if err != nil {
		return nil, err
	}
	env, err := decodeEAPIObjectChecked(body, "search")
	if err != nil {
		return nil, err
	}
	if err := eapiFailure(env); err != nil {
		return nil, err
	}
	return c.bookPageFromEnv(env, page, count)
}

// mergeFilter unifies the slice and single-value forms, dropping the GUI's
// "全部"/"all" sentinel and duplicates.
func mergeFilter(list []string, single string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(list)+1)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || strings.EqualFold(v, "全部") || strings.EqualFold(v, "all") {
			return
		}
		key := strings.ToLower(v)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, v)
	}
	for _, v := range list {
		add(v)
	}
	add(single)
	return out
}

// bookPageFromEnv maps a list response. pagination.current is optional and
// falls back to the requested page so paging never deadlocks.
func (c *Client) bookPageFromEnv(env map[string]any, page, count int) (*BookPage, error) {
	pageInfo := asMap(env["pagination"])
	current := page
	totalPages := 0
	if pageInfo != nil {
		if v := eapiInt(pageInfo["current"]); v > 0 {
			current = v
		}
		totalPages = eapiInt(pageInfo["total_pages"])
	}

	books := make([]Book, 0)
	raw, _ := env["books"].([]any)
	for _, item := range raw {
		m := asMap(item)
		if m == nil {
			continue
		}
		books = append(books, c.mapEAPIBook(m, "", ""))
	}

	total := 0
	if totalPages > 0 {
		total = totalPages * count
	}
	return &BookPage{Books: books, Page: current, Total: total, TotalPages: totalPages}, nil
}

// FetchBook loads GET /eapi/book/{id}/{hash}. Missing id/hash in the body
// are filled from the reference.
func (c *Client) FetchBook(ctx context.Context, id, hash string) (*Book, error) {
	if id == "" || hash == "" {
		return nil, ErrNoID
	}
	path := "/eapi/book/" + url.PathEscape(id) + "/" + url.PathEscape(hash)
	body, err := c.eapiRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	env, err := decodeEAPIObjectChecked(body, "book")
	if err != nil {
		return nil, err
	}
	if err := eapiFailure(env); err != nil {
		return nil, err
	}
	raw := asMap(env["book"])
	if raw == nil {
		return nil, fmt.Errorf("invalid EAPI book response: missing book object")
	}
	book := c.mapEAPIBook(raw, id, hash)
	return &book, nil
}

// parseEAPIBookRef splits "id:hash". Both halves must be non-empty.
func parseEAPIBookRef(ref string) (id, hash string, err error) {
	ref = strings.TrimSpace(ref)
	id, hash, ok := strings.Cut(ref, ":")
	id = strings.TrimSpace(id)
	hash = strings.TrimSpace(hash)
	if !ok || id == "" || hash == "" {
		return "", "", ErrNoID
	}
	return id, hash, nil
}

var eapiFilePathPattern = regexp.MustCompile(`^/eapi/book/[^/]+/[^/]+/file/?$`)

// resolveEAPIFileURL turns /eapi/book/{id}/{hash}/file into the real CDN
// link. Already-resolved CDN links (e.g. from history) pass through.
func (c *Client) resolveEAPIFileURL(ctx context.Context, rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("%w: empty download URL", ErrDownloadFailed)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, nil
	}
	_, origin := c.domainSnapshot()
	if origin == nil ||
		u.Scheme != origin.Scheme ||
		!strings.EqualFold(u.Hostname(), origin.Hostname()) ||
		!eapiFilePathPattern.MatchString(u.Path) {
		return rawURL, nil
	}

	body, err := c.eapiRequest(ctx, http.MethodGet, u.Path, nil)
	if err != nil {
		return "", err
	}
	env, err := decodeEAPIObjectChecked(body, "file")
	if err != nil {
		return "", err
	}
	if err := eapiFailure(env); err != nil {
		return "", err
	}

	link := ""
	if file := asMap(env["file"]); file != nil {
		link = eapiString(file["downloadLink"])
		if link == "" {
			link = eapiString(file["link"])
		}
		if link == "" {
			link = eapiString(file["url"])
		}
	}
	if link == "" {
		link = eapiString(env["downloadLink"])
	}
	if link == "" {
		link = eapiString(env["url"])
	}
	if link == "" {
		link = eapiString(env["link"])
	}
	if link == "" {
		return "", fmt.Errorf("%w: EAPI returned no file link", ErrDownloadFailed)
	}
	return c.absolutizeURL(link), nil
}

// mapEAPIBook is the single mapping entry point shared by every endpoint.
func (c *Client) mapEAPIBook(raw map[string]any, fallbackID, fallbackHash string) Book {
	id := eapiString(raw["id"])
	if id == "" {
		id = fallbackID
	}
	hash := eapiString(raw["hash"])
	if hash == "" {
		hash = fallbackHash
	}

	title := eapiString(raw["title"])
	author := eapiString(raw["author"])
	authors := []string{}
	if author != "" {
		authors = append(authors, author)
	}

	size := eapiString(raw["filesizeString"])
	if size == "" {
		size = eapiString(raw["filesize"])
	}

	pagesText := eapiString(raw["pages"])
	if pagesText == "0" {
		pagesText = ""
	}

	isbn := eapiString(raw["identifier"])
	if isbn == "" {
		isbn = eapiString(raw["isbn"])
	}
	isbn10 := eapiString(raw["isbn10"])
	isbn13 := eapiString(raw["isbn13"])
	isbn10, isbn13 = normalizeISBN(isbn, isbn10, isbn13)

	date := eapiString(raw["date"])
	if date == "" {
		date = eapiString(raw["downloaded_at"])
	}

	domain, _ := c.domainSnapshot()
	downloadURL := ""
	if id != "" && hash != "" {
		downloadURL = domain + "/eapi/book/" + url.PathEscape(id) + "/" + url.PathEscape(hash) + "/file"
	} else if dl := eapiString(raw["dl"]); dl != "" {
		downloadURL = c.absolutizeURL(dl)
	}

	return Book{
		ID:          id,
		Hash:        hash,
		ISBN:        isbn,
		ISBN10:      isbn10,
		ISBN13:      isbn13,
		URL:         c.absolutizeURL(eapiString(raw["href"])),
		Cover:       c.absolutizeURL(eapiString(raw["cover"])),
		Title:       title,
		Author:      author,
		Authors:     authors,
		Publisher:   eapiString(raw["publisher"]),
		Year:        eapiString(raw["year"]),
		Language:    eapiString(raw["language"]),
		Extension:   strings.ToUpper(eapiString(raw["extension"])),
		Size:        size,
		Rating:      eapiFloat(raw["interestScore"]),
		Quality:     eapiString(raw["qualityScore"]),
		Description: eapiString(raw["description"]),
		Categories:  eapiCategories(raw["categories"], raw["category"]),
		Edition:     eapiString(raw["edition"]),
		Pages:       eapiInt(pagesText),
		DownloadURL: downloadURL,
		Date:        date,
	}
}

// eapiCategories flattens categories recursively: strings pass through,
// arrays join with ", ", objects yield their name/title/path.
func eapiCategories(values ...any) string {
	parts := make([]string, 0, 4)
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case nil:
		case string:
			if s := strings.TrimSpace(t); s != "" {
				parts = append(parts, s)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		case map[string]any:
			for _, key := range []string{"name", "title", "path"} {
				if s := eapiString(t[key]); s != "" {
					parts = append(parts, s)
					return
				}
			}
		default:
			if s := eapiString(v); s != "" {
				parts = append(parts, s)
			}
		}
	}
	for _, v := range values {
		walk(v)
	}
	return strings.Join(parts, ", ")
}

// normalizeISBN validates ISBN-10/13 and derives the missing form from a
// valid one. Invalid values are dropped rather than shown.
func normalizeISBN(isbn, isbn10, isbn13 string) (string, string) {
	clean := func(s string) string {
		s = strings.ToUpper(strings.TrimSpace(s))
		replacer := strings.NewReplacer("-", "", " ", "", "_", "")
		return replacer.Replace(s)
	}
	isbn10, isbn13 = clean(isbn10), clean(isbn13)
	base := clean(isbn)

	if !validISBN10(isbn10) {
		isbn10 = ""
	}
	if !validISBN13(isbn13) {
		isbn13 = ""
	}
	if isbn10 == "" && validISBN10(base) {
		isbn10 = base
	}
	if isbn13 == "" && validISBN13(base) {
		isbn13 = base
	}
	if isbn13 == "" && isbn10 != "" {
		isbn13 = isbn10To13(isbn10)
	}
	if isbn10 == "" && isbn13 != "" && strings.HasPrefix(isbn13, "978") {
		isbn10 = isbn13To10(isbn13)
	}
	return isbn10, isbn13
}

func validISBN10(s string) bool {
	if len(s) != 10 {
		return false
	}
	sum := 0
	for i := 0; i < 10; i++ {
		ch := s[i]
		var d int
		switch {
		case ch >= '0' && ch <= '9':
			d = int(ch - '0')
		case ch == 'X' && i == 9:
			d = 10
		default:
			return false
		}
		sum += (10 - i) * d
	}
	return sum%11 == 0
}

func validISBN13(s string) bool {
	if len(s) != 13 {
		return false
	}
	sum := 0
	for i := 0; i < 13; i++ {
		ch := s[i]
		if ch < '0' || ch > '9' {
			return false
		}
		d := int(ch - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return sum%10 == 0
}

func isbn10To13(isbn10 string) string {
	body := "978" + isbn10[:9]
	sum := 0
	for i := 0; i < 12; i++ {
		d := int(body[i] - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	check := (10 - sum%10) % 10
	return body + strconv.Itoa(check)
}

func isbn13To10(isbn13 string) string {
	body := isbn13[3:12]
	sum := 0
	for i := 0; i < 9; i++ {
		sum += (10 - i) * int(body[i]-'0')
	}
	check := (11 - sum%11) % 11
	if check == 10 {
		return body + "X"
	}
	return body + strconv.Itoa(check)
}
