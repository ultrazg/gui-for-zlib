package core

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Demo is an in-memory Service for the GUI skeleton. It simulates network
// latency, paging and cancellable downloads, but never touches the network
// or the disk. Replace it with the real client when the core layer is
// ready.
type Demo struct {
	books []Book

	mu    sync.Mutex
	email string
	name  string
	saved map[string]map[string]bool
}

// NewDemo returns the sample backend.
func NewDemo() *Demo {
	return &Demo{books: demoBooks(), saved: map[string]map[string]bool{}}
}

func delay(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Demo) Login(ctx context.Context, email, password string) (*Session, error) {
	if err := delay(ctx, 650*time.Millisecond); err != nil {
		return nil, err
	}
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") || len(password) < 3 {
		return nil, ErrInvalidCredentials
	}
	name := email[:strings.IndexByte(email, '@')]
	d.mu.Lock()
	d.email, d.name = email, name
	d.mu.Unlock()
	return &Session{Email: email, Name: name}, nil
}

func (d *Demo) Search(ctx context.Context, opt SearchOptions) (*BookPage, error) {
	if err := delay(ctx, 420*time.Millisecond); err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(opt.Query))
	out := make([]Book, 0, len(d.books))
	for _, b := range d.books {
		if q != "" {
			hay := strings.ToLower(b.Title + " " + b.Author + " " + b.Description)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		if opt.Extension != "" && !strings.EqualFold(opt.Extension, "全部") &&
			!strings.EqualFold(opt.Extension, b.Extension) {
			continue
		}
		out = append(out, b)
	}
	return pageOf(out, opt.Page, opt.Count), nil
}

func (d *Demo) Favorites(ctx context.Context, page, count int) (*BookPage, error) {
	if err := delay(ctx, 380*time.Millisecond); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	saved := d.savedBooksLocked()
	books := make([]Book, 0, len(saved))
	for _, book := range d.books {
		if saved[book.ID] {
			value := true
			book.Saved = &value
			books = append(books, book)
		}
	}
	return pageOf(books, page, count), nil
}

func (d *Demo) FavoriteStatus(ctx context.Context, book Book) (bool, error) {
	if err := delay(ctx, 60*time.Millisecond); err != nil {
		return false, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.email == "" {
		return false, ErrNotLoggedIn
	}
	return d.savedBooksLocked()[book.ID], nil
}

func (d *Demo) SetFavorite(ctx context.Context, id string, saved bool) error {
	id = normalizeDigits(id)
	if id == "" {
		return ErrNoID
	}
	if err := delay(ctx, 120*time.Millisecond); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.email == "" {
		return ErrNotLoggedIn
	}
	if saved {
		d.savedBooksLocked()[id] = true
	} else {
		delete(d.savedBooksLocked(), id)
	}
	return nil
}

// savedBooksLocked keeps each demo account's collection across sign-ins.
func (d *Demo) savedBooksLocked() map[string]bool {
	account := d.email
	if account == "" {
		account = "demo@example.com"
	}
	if d.saved == nil {
		d.saved = map[string]map[string]bool{}
	}
	if d.saved[account] == nil {
		books := map[string]bool{}
		for _, book := range pick(d.books, 0, 2, 5, 8, 11) {
			books[book.ID] = true
		}
		d.saved[account] = books
	}
	return d.saved[account]
}

func (d *Demo) History(ctx context.Context, page, count int) (*BookPage, error) {
	if err := delay(ctx, 340*time.Millisecond); err != nil {
		return nil, err
	}
	return pageOf(pick(d.books, 1, 3, 4, 7, 9, 12, 13), page, count), nil
}

func (d *Demo) Recommended(ctx context.Context, page, count int) (*BookPage, error) {
	if err := delay(ctx, 460*time.Millisecond); err != nil {
		return nil, err
	}
	return pageOf(pick(d.books, 2, 4, 6, 10), page, count), nil
}

func (d *Demo) Popular(ctx context.Context, page, count int) (*BookPage, error) {
	if err := delay(ctx, 460*time.Millisecond); err != nil {
		return nil, err
	}
	return pageOf(pick(d.books, 0, 3, 6, 9, 12), page, count), nil
}

func (d *Demo) Profile(ctx context.Context) (*Profile, error) {
	if err := delay(ctx, 300*time.Millisecond); err != nil {
		return nil, err
	}
	d.mu.Lock()
	email, name := d.email, d.name
	favorites := len(d.savedBooksLocked())
	d.mu.Unlock()
	if email == "" {
		email, name = "demo@example.com", "demo"
	}
	return &Profile{
		Email:          email,
		Name:           name,
		DownloadsLeft:  7,
		DownloadsTotal: 10,
		Favorites:      favorites,
		Downloads:      23,
	}, nil
}

func (d *Demo) Doctor(ctx context.Context) (*DoctorReport, error) {
	if err := delay(ctx, 900*time.Millisecond); err != nil {
		return nil, err
	}
	return &DoctorReport{
		Checked: time.Now(),
		Domains: []DomainStatus{
			{Domain: "api.zlib-demo.org", OK: true, Latency: 86 * time.Millisecond},
			{Domain: "login.zlib-demo.org", OK: true, Latency: 112 * time.Millisecond},
			{Domain: "download.zlib-demo.org", OK: true, Latency: 143 * time.Millisecond},
			{Domain: "backup.zlib-demo.org", OK: false, Latency: 2 * time.Second, Error: "timeout"},
		},
	}, nil
}

// FetchImage returns what the real backend would, but demo books carry no
// covers, so there is nothing to fetch.
func (d *Demo) FetchImage(ctx context.Context, url string) ([]byte, error) {
	return nil, ErrUnsupported
}

func (d *Demo) Download(ctx context.Context, book Book, dir string, progress func(done, total int64)) (string, error) {
	const (
		total = 4_400_000
		steps = 28
	)
	if progress != nil {
		progress(0, total)
	}
	for i := 1; i <= steps; i++ {
		if err := delay(ctx, 70*time.Millisecond); err != nil {
			return "", err
		}
		if progress != nil {
			progress(int64(float64(total)*float64(i)/steps), total)
		}
	}
	ext := strings.ToLower(book.Extension)
	if ext == "" {
		ext = "bin"
	}
	return filepath.Join(dir, sanitize(book.Title)+"."+ext), nil
}

// CurrentSession returns the session created by the last demo login.
func (d *Demo) CurrentSession() *Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.email == "" {
		return nil
	}
	return &Session{Email: d.email, Name: d.name}
}

// Logout forgets the demo session.
func (d *Demo) Logout() error {
	d.mu.Lock()
	d.email, d.name = "", ""
	d.mu.Unlock()
	return nil
}

func pageOf(books []Book, page, count int) *BookPage {
	if page < 1 {
		page = 1
	}
	if count <= 0 {
		count = 10
	}
	start := (page - 1) * count
	if start > len(books) {
		start = len(books)
	}
	end := start + count
	if end > len(books) {
		end = len(books)
	}
	return &BookPage{Books: books[start:end], Page: page, Total: len(books)}
}

func pick(books []Book, idx ...int) []Book {
	out := make([]Book, 0, len(idx))
	for _, i := range idx {
		if i >= 0 && i < len(books) {
			out = append(out, books[i])
		}
	}
	return out
}

func sanitize(name string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	name = strings.TrimSpace(r.Replace(name))
	if name == "" {
		return "book"
	}
	return name
}

func demoBooks() []Book {
	return []Book{
		{
			ID: "1001", Hash: "a1b2c3", Title: "Go 并发模式实践", Author: "林见川",
			Year: "2024", Publisher: "示例出版社", Language: "中文", Extension: "EPUB",
			Size: "3.8 MB", Rating: 4.8, Pages: 412,
			Description: "从 goroutine、channel 到 errgroup 与结构化并发，配合可运行的示例讲清 Go 并发设计的取舍。",
		},
		{
			ID: "1002", Hash: "d4e5f6", Title: "Distributed Systems Notes", Author: "Ada Lin",
			Year: "2023", Publisher: "Sample Press", Language: "English", Extension: "PDF",
			Size: "12.4 MB", Rating: 4.6, Pages: 688,
			Description: "A compact tour of replication, consensus, consistency models and failure handling, with diagrams and exercises.",
		},
		{
			ID: "1003", Hash: "789abc", Title: "银河边缘的图书馆", Author: "周牧",
			Year: "2025", Publisher: "星海文艺", Language: "中文", Extension: "EPUB",
			Size: "2.1 MB", Rating: 4.9, Pages: 296,
			Description: "一座漂流在轨道上的图书馆，和一位负责归还逾期书籍的管理员。",
		},
		{
			ID: "1004", Hash: "def012", Title: "界面之下：GUI 框架设计", Author: "陈默",
			Year: "2024", Publisher: "示例出版社", Language: "中文", Extension: "PDF",
			Size: "8.7 MB", Rating: 4.5, Pages: 534,
			Description: "即时模式与保留模式、布局、焦点、文本输入和渲染管线，一本写给应用开发者的框架内幕。",
		},
		{
			ID: "1005", Hash: "345678", Title: "The Quiet Algorithm", Author: "Mira Stone",
			Year: "2022", Publisher: "Northwind", Language: "English", Extension: "MOBI",
			Size: "1.6 MB", Rating: 4.3, Pages: 244,
			Description: "A novel about a programmer who finds a sorting routine that seems to know what people want.",
		},
		{
			ID: "1006", Hash: "901234", Title: "从零实现一个下载器", Author: "许行",
			Year: "2023", Publisher: "实践技术", Language: "中文", Extension: "EPUB",
			Size: "4.2 MB", Rating: 4.7, Pages: 368,
			Description: "重试、断点续传、原子落盘、进度节流与取消：把下载这件小事做对。",
		},
		{
			ID: "1007", Hash: "567890", Title: "Notes on Type Systems", Author: "Peter Vale",
			Year: "2021", Publisher: "Sample Press", Language: "English", Extension: "PDF",
			Size: "6.9 MB", Rating: 4.4, Pages: 402,
			Description: "Inference, subtyping, effects and the practical cost of expressive types.",
		},
		{
			ID: "1008", Hash: "bcdef0", Title: "夜航船手记", Author: "沈知白",
			Year: "2020", Publisher: "星海文艺", Language: "中文", Extension: "EPUB",
			Size: "1.9 MB", Rating: 4.2, Pages: 218,
			Description: "沿海小镇的旧船、灯塔与口述史，一本缓慢的旅行笔记。",
		},
		{
			ID: "1009", Hash: "123456", Title: "Writing a Desktop App", Author: "Nora Field",
			Year: "2025", Publisher: "Northwind", Language: "English", Extension: "EPUB",
			Size: "3.1 MB", Rating: 4.6, Pages: 330,
			Description: "From the first window to packaging, updates and the small details users notice.",
		},
		{
			ID: "1010", Hash: "cdef01", Title: "协议之美", Author: "顾南",
			Year: "2024", Publisher: "实践技术", Language: "中文", Extension: "PDF",
			Size: "9.3 MB", Rating: 4.8, Pages: 576,
			Description: "从请求与响应出发，理解认证、重定向、缓存、限流和错误分类。",
		},
		{
			ID: "1011", Hash: "ef0123", Title: "The Lighthouse Index", Author: "Iris Cole",
			Year: "2023", Publisher: "Northwind", Language: "English", Extension: "AZW3",
			Size: "2.7 MB", Rating: 4.1, Pages: 280,
			Description: "A librarian catalogues lighthouses, storms and the people who keep them.",
		},
		{
			ID: "1012", Hash: "012345", Title: "小城书店的最后一夜", Author: "苏遥",
			Year: "2022", Publisher: "星海文艺", Language: "中文", Extension: "MOBI",
			Size: "1.4 MB", Rating: 4.4, Pages: 206,
			Description: "关店前的十二个小时，十二位客人，以及一本始终没有卖出去的书。",
		},
		{
			ID: "1013", Hash: "234567", Title: "Testing Without a Window", Author: "Sam Reed",
			Year: "2024", Publisher: "Sample Press", Language: "English", Extension: "EPUB",
			Size: "2.3 MB", Rating: 4.5, Pages: 264,
			Description: "Headless UI tests, golden frames and the habits that keep interfaces honest.",
		},
		{
			ID: "1014", Hash: "456789", Title: "字体、留白与阅读", Author: "唐可",
			Year: "2025", Publisher: "示例出版社", Language: "中文", Extension: "EPUB",
			Size: "5.6 MB", Rating: 4.7, Pages: 348,
			Description: "中文界面的排版基础：字重、行高、栅格和深色模式下的对比度。",
		},
	}
}
