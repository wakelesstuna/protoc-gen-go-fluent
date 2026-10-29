package fluent

import (
	"context"
	"iter"
)

// Page is one page of results.
type Page[T any] struct {
	Items []T
	// NextPageToken fetches the following page; empty on the last page.
	NextPageToken string
}

// Paging holds the page size and overall limit of a query.
type Paging struct {
	// PageSize is how many items each request asks for; 0 means the server default.
	PageSize int32
	// Limit caps the total number of items All yields; 0 means no limit.
	Limit int
}

// FetchFunc fetches one page of up to pageSize items (0 means the server default).
type FetchFunc[T any] func(ctx context.Context, pageSize int32, pageToken string) (Page[T], error)

// All iterates over every item across pages, fetching each page as it is reached.
// It stops after p.Limit items when set, shrinking the last request so it does not
// over-fetch. Breaking out of the loop stops further requests. On error it yields
// the zero value and the error, then stops.
func All[T any](ctx context.Context, p Paging, fetch FetchFunc[T]) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		token := ""
		seen := 0
		for {
			size := p.PageSize
			if p.Limit > 0 {
				remaining := int32(p.Limit - seen)
				if size == 0 || remaining < size {
					size = remaining
				}
			}

			page, err := fetch(ctx, size, token)
			if err != nil {
				yield(zero, err)
				return
			}
			for _, item := range page.Items {
				if !yield(item, nil) {
					return
				}
				seen++
				if p.Limit > 0 && seen >= p.Limit {
					return
				}
			}
			if page.NextPageToken == "" {
				return
			}
			token = page.NextPageToken
		}
	}
}

// Collect gathers seq into a slice, returning the first error.
func Collect[T any](seq iter.Seq2[T, error]) ([]T, error) {
	var out []T
	for item, err := range seq {
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}
