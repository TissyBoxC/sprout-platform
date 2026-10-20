package service

// Page is a bounded listing window. A zero Limit means the default page size,
// and a negative Offset is treated as zero.
type Page struct {
	Limit  int
	Offset int
}

const (
	// DefaultPageSize is the page size a listing uses when the caller omits one.
	DefaultPageSize = 20
	// MaxPageSize bounds one page so a caller cannot ask for the whole table.
	MaxPageSize = 200
)

// normalized clamps the window into the supported range.
func (p Page) normalized() Page {
	limit := p.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	return Page{Limit: limit, Offset: offset}
}
