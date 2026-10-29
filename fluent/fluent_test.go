package fluent

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"google.golang.org/grpc/metadata"
)

// numbers serves the integers [0, total) in pages and records each requested page size.
type numbers struct {
	total int
	sizes []int32
}

func (n *numbers) fetch(_ context.Context, pageSize int32, token string) (Page[int], error) {
	n.sizes = append(n.sizes, pageSize)
	start, _ := strconv.Atoi(token)
	size := int(pageSize)
	if size == 0 {
		size = 10
	}
	end := min(start+size, n.total)
	var p Page[int]
	for i := start; i < end; i++ {
		p.Items = append(p.Items, i)
	}
	if end < n.total {
		p.NextPageToken = strconv.Itoa(end)
	}
	return p, nil
}

func TestAllFollowsPages(t *testing.T) {
	n := &numbers{total: 7}
	got, err := Collect(All(context.Background(), Paging{PageSize: 3}, n.fetch))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 || got[6] != 6 {
		t.Errorf("got %v", got)
	}
	if len(n.sizes) != 3 {
		t.Errorf("made %d requests, want 3", len(n.sizes))
	}
}

func TestAllLimitShrinksLastRequest(t *testing.T) {
	n := &numbers{total: 100}
	got, err := Collect(All(context.Background(), Paging{PageSize: 10, Limit: 15}, n.fetch))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 15 {
		t.Fatalf("got %d items, want 15", len(got))
	}
	if len(n.sizes) != 2 || n.sizes[1] != 5 {
		t.Errorf("page sizes = %v, want [10 5]", n.sizes)
	}
}

func TestAllLimitWithoutPageSize(t *testing.T) {
	n := &numbers{total: 100}
	if _, err := Collect(All(context.Background(), Paging{Limit: 4}, n.fetch)); err != nil {
		t.Fatal(err)
	}
	if len(n.sizes) != 1 || n.sizes[0] != 4 {
		t.Errorf("page sizes = %v, want [4]", n.sizes)
	}
}

func TestAllBreakStopsFetching(t *testing.T) {
	n := &numbers{total: 100}
	for i := range All(context.Background(), Paging{PageSize: 10}, n.fetch) {
		if i == 2 {
			break
		}
	}
	if len(n.sizes) != 1 {
		t.Errorf("made %d requests, want 1", len(n.sizes))
	}
}

func TestAllStopsOnError(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	fetch := func(context.Context, int32, string) (Page[int], error) {
		calls++
		if calls == 2 {
			return Page[int]{}, boom
		}
		return Page[int]{Items: []int{1}, NextPageToken: "next"}, nil
	}
	got, err := Collect(All(context.Background(), Paging{}, fetch))
	if !errors.Is(err, boom) || got != nil {
		t.Errorf("got %v, %v; want nil, boom", got, err)
	}
	if calls != 2 {
		t.Errorf("made %d calls, want 2", calls)
	}
}

func TestConnWithChainsWithoutSharing(t *testing.T) {
	base := NewConn(nil).With(Metadata("a", "1"))
	// Two children of the same parent must not overwrite each other's functions.
	x := base.With(Metadata("b", "x"))
	y := base.With(Metadata("b", "y"))

	check := func(c *Conn, want map[string]string) {
		t.Helper()
		md, _ := metadata.FromOutgoingContext(c.Outgoing(context.Background()))
		if len(md) != len(want) {
			t.Errorf("metadata = %v, want %v", md, want)
		}
		for k, v := range want {
			if got := md.Get(k); len(got) != 1 || got[0] != v {
				t.Errorf("%s = %v, want %q", k, got, v)
			}
		}
	}
	check(base, map[string]string{"a": "1"})
	check(x, map[string]string{"a": "1", "b": "x"})
	check(y, map[string]string{"a": "1", "b": "y"})
}

func TestCloseOnlyClosesOwnedConn(t *testing.T) {
	if err := NewConn(nil).Close(); err != nil {
		t.Errorf("Close on borrowed conn = %v", err)
	}
	if err := NewConn(nil).With().Close(); err != nil {
		t.Errorf("Close on derived conn = %v", err)
	}
}
