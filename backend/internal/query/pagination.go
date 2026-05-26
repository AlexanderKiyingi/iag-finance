package query

import (
	"net/http"
	"strconv"
)

type Page struct {
	Page   int `json:"page"`
	Limit  int `json:"limit"`
	Offset int `json:"-"`
	Total  int `json:"total"`
}

func ParsePage(r *http.Request, defaultLimit, maxLimit int) Page {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return Page{Page: page, Limit: limit, Offset: (page - 1) * limit}
}

func SlicePage[T any](items []T, p Page) ([]T, Page) {
	total := len(items)
	p.Total = total
	if p.Offset >= total {
		return []T{}, p
	}
	end := p.Offset + p.Limit
	if end > total {
		end = total
	}
	return items[p.Offset:end], p
}
