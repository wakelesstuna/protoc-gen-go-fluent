package libraryclient_test

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/wakelesstuna/protoc-gen-go-fluent/example/libraryclient"
	"github.com/wakelesstuna/protoc-gen-go-fluent/fluent"
)

func Example() {
	// Dial once per process and share the client.
	c, err := libraryclient.Dial("library.example.com:443",
		grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()

	// Per request: derive a client that adds the caller's credentials to every call.
	user := c.With(fluent.Metadata("authorization", "Bearer "+"<token>"))

	// Iterate every shelf; pages are fetched as the loop reaches them.
	for shelf, err := range user.Shelves().All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(shelf.GetId(), shelf.GetTheme())
	}

	// Navigate to a shelf, filter its books, cap the result.
	books, err := user.Shelf("sci-fi").Books().
		Author("Ursula K. Le Guin").
		OnlyScienceFiction().
		Limit(20).
		Collect(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(books))

	// Page through manually, e.g. to hand page tokens to a web UI.
	page, err := user.Shelf("sci-fi").Book("left-hand-of-darkness").Reviews().
		MinStars(4).
		PageSize(10).
		Page(ctx, "")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(page.Items), page.NextPageToken)
}
