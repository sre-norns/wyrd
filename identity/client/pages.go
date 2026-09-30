package client

import (
	"context"
	"iter"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// All yields every item of a list, following each page's next cursor from
// query's position to the last page. list is any List method of this client
// bound to its path arguments. Iteration stops at the first error, which is
// yielded with a zero item.
func All[T any](ctx context.Context, query manifest.SearchQuery, list func(context.Context, manifest.SearchQuery) ([]T, manifest.Page, error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			items, page, err := list(ctx, query)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
			if page.Next == "" {
				return
			}
			query.Cursor = page.Next
		}
	}
}

// Collect gathers every item of a list; see [All].
func Collect[T any](ctx context.Context, query manifest.SearchQuery, list func(context.Context, manifest.SearchQuery) ([]T, manifest.Page, error)) ([]T, error) {
	out := []T{}
	for item, err := range All(ctx, query, list) {
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}
