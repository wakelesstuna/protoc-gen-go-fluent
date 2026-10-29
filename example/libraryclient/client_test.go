package libraryclient

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	libraryv1 "github.com/wakelesstuna/protoc-gen-go-fluent/example/gen/library/v1"
	"github.com/wakelesstuna/protoc-gen-go-fluent/fluent"
)

// fakeLibrary serves total numbered items per list RPC and records requests and metadata.
type fakeLibrary struct {
	libraryv1.UnimplementedLibraryServiceServer
	total int

	mu       sync.Mutex
	requests []proto.Message
	metadata []metadata.MD
}

func (f *fakeLibrary) record(ctx context.Context, req proto.Message) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	f.metadata = append(f.metadata, md)
}

func (f *fakeLibrary) window(pageSize int32, token string) (int, int, string) {
	start, _ := strconv.Atoi(token)
	size := int(pageSize)
	if size == 0 {
		size = 10
	}
	end := min(start+size, f.total)
	next := ""
	if end < f.total {
		next = strconv.Itoa(end)
	}
	return start, end, next
}

func (f *fakeLibrary) ListShelves(ctx context.Context, req *libraryv1.ListShelvesRequest) (*libraryv1.ListShelvesResponse, error) {
	f.record(ctx, req)
	start, end, next := f.window(req.GetPageSize(), req.GetPageToken())
	resp := &libraryv1.ListShelvesResponse{NextPageToken: next}
	for i := start; i < end; i++ {
		resp.Shelves = append(resp.Shelves, &libraryv1.Shelf{Id: fmt.Sprintf("s%d", i)})
	}
	return resp, nil
}

func (f *fakeLibrary) ListBooks(ctx context.Context, req *libraryv1.ListBooksRequest) (*libraryv1.ListBooksResponse, error) {
	f.record(ctx, req)
	if req.GetShelfId() == "missing" {
		return nil, status.Error(codes.NotFound, "no such shelf")
	}
	start, end, next := f.window(req.GetPageSize(), req.GetPageToken())
	resp := &libraryv1.ListBooksResponse{NextPageToken: next}
	for i := start; i < end; i++ {
		resp.Books = append(resp.Books, &libraryv1.Book{Id: fmt.Sprintf("b%d", i)})
	}
	return resp, nil
}

func (f *fakeLibrary) ListReviews(ctx context.Context, req *libraryv1.ListReviewsRequest) (*libraryv1.ListReviewsResponse, error) {
	f.record(ctx, req)
	start, end, next := f.window(req.GetPageSize(), req.GetPageToken())
	resp := &libraryv1.ListReviewsResponse{NextPageToken: next}
	for i := start; i < end; i++ {
		resp.Reviews = append(resp.Reviews, &libraryv1.Review{Id: fmt.Sprintf("r%d", i)})
	}
	return resp, nil
}

func newTestClient(t *testing.T, total int) (*Client, *fakeLibrary) {
	t.Helper()
	fake := &fakeLibrary{total: total}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	libraryv1.RegisterLibraryServiceServer(srv, fake)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	c, err := Dial("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, fake
}

func TestNavigationFillsIDs(t *testing.T) {
	c, fake := newTestClient(t, 1)
	if _, err := c.Shelf("s1").Book("b9").Reviews().MinStars(4).Page(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	want := &libraryv1.ListReviewsRequest{ShelfId: "s1", BookId: "b9", MinStars: 4}
	if !proto.Equal(fake.requests[0], want) {
		t.Errorf("request = %v, want %v", fake.requests[0], want)
	}
}

func TestSettersAndShortcuts(t *testing.T) {
	c, fake := newTestClient(t, 1)
	if _, err := c.Shelf("s1").Books().Author("Le Guin").Title("Earthsea").OnlyFiction().Page(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	want := &libraryv1.ListBooksRequest{ShelfId: "s1", Author: "Le Guin", TitleContains: "Earthsea", Genre: libraryv1.Genre_GENRE_FICTION}
	if !proto.Equal(fake.requests[0], want) {
		t.Errorf("request = %v, want %v", fake.requests[0], want)
	}
}

func TestCollectAcrossPagesWithLimit(t *testing.T) {
	c, fake := newTestClient(t, 50)
	shelves, err := c.Shelves().PageSize(4).Limit(10).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(shelves) != 10 || shelves[9].GetId() != "s9" {
		t.Fatalf("got %d shelves", len(shelves))
	}
	// 4 + 4 + 2: the last request shrinks to what the limit still needs.
	if len(fake.requests) != 3 || fake.requests[2].(*libraryv1.ListShelvesRequest).GetPageSize() != 2 {
		t.Errorf("requests = %v", fake.requests)
	}
}

func TestBuildersAreImmutable(t *testing.T) {
	c, fake := newTestClient(t, 1)
	ctx := context.Background()
	base := c.Shelf("s1").Books().Author("A")
	if _, err := base.OnlyNonFiction().Page(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Page(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if g := fake.requests[1].(*libraryv1.ListBooksRequest).GetGenre(); g != libraryv1.Genre_GENRE_UNSPECIFIED {
		t.Errorf("base query picked up genre %v from a derived query", g)
	}
}

func TestWithAddsMetadataPerClient(t *testing.T) {
	c, fake := newTestClient(t, 1)
	ctx := context.Background()
	alice := c.With(fluent.Metadata("authorization", "Bearer alice"))

	if _, err := alice.Shelves().Page(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Shelves().Page(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.metadata[0].Get("authorization"); len(got) != 1 || got[0] != "Bearer alice" {
		t.Errorf("alice authorization = %v", got)
	}
	if got := fake.metadata[1].Get("authorization"); len(got) != 0 {
		t.Errorf("base client sent authorization %v", got)
	}
	// Closing a derived client must leave the shared connection open.
	if err := alice.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Shelves().Page(ctx, ""); err != nil {
		t.Errorf("shared connection closed by derived client: %v", err)
	}
}

func TestErrorsAreStatusErrors(t *testing.T) {
	c, _ := newTestClient(t, 1)
	_, err := c.Shelf("missing").Books().Collect(context.Background())
	if status.Code(err) != codes.NotFound {
		t.Errorf("err = %v, want NotFound", err)
	}
}
