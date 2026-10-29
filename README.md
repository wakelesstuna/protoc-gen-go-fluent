# protoc-gen-go-fluent

A `protoc` / `buf` plugin that turns annotated gRPC services into **fluent,
paginated Go clients**.

Instead of this:

```go
stub := libraryv1.NewLibraryServiceClient(conn)
ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
var books []*libraryv1.Book
pageToken := ""
for {
	resp, err := stub.ListBooks(ctx, &libraryv1.ListBooksRequest{
		ShelfId:   "sci-fi",
		Author:    "Ursula K. Le Guin",
		Genre:     libraryv1.Genre_GENRE_SCIENCE_FICTION,
		PageSize:  20,
		PageToken: pageToken,
	})
	if err != nil {
		return err
	}
	books = append(books, resp.GetBooks()...)
	if resp.GetNextPageToken() == "" || len(books) >= 20 {
		break
	}
	pageToken = resp.GetNextPageToken()
}
books = books[:min(len(books), 20)]
```

you write this:

```go
books, err := client.With(fluent.Metadata("authorization", "Bearer "+token)).
	Shelf("sci-fi").Books().
	Author("Ursula K. Le Guin").
	OnlyScienceFiction().
	Limit(20).
	Collect(ctx)
```

## What you can use it for

- **Internal SDKs for your gRPC APIs.** Give the teams that call your service a typed,
  discoverable client. IDE autocomplete walks them through the resource tree
  (`Shelf(id).Book(id).Reviews()`) and the available filters.
- **APIs with nested resources.** For example projects → repositories → commits,
  organizations → teams → members, or tenants → clusters → nodes. Parent ids are
  carried along, so callers never repeat them.
- **Hiding pagination.** `All` streams items across pages with a Go iterator, `Collect`
  gathers them into a slice, and `Page` gives you manual control (e.g. for a web UI).
  `Limit` caps the total, and the last request shrinks so it doesn't over-fetch.
- **Per-request credentials on one shared connection.** Dial once, then
  `client.With(...)` derives a cheap per-user client that adds auth or tenant headers
  to every call.

It only generates code for list RPCs you mark, so you can add it to existing APIs
gradually. Plain gRPC stubs keep working next to it.

## Requirements

- Go 1.24+ (iterators)
- [buf](https://buf.build/docs/installation) (recommended) or `protoc`
- `protoc-gen-go` and `protoc-gen-go-grpc` for the regular message and stub code
- List RPCs that follow [AIP-158](https://google.aip.dev/158) pagination: `page_size`
  (int32) and `page_token` (string) in the request, and `next_page_token` (string) in
  the response

## Quick start

### 1. Install the plugin

```sh
go install github.com/wakelesstuna/protoc-gen-go-fluent@latest
```

Or pin it per project with Go's tool directive. buf then runs it with `go tool`:

```sh
go get -tool github.com/wakelesstuna/protoc-gen-go-fluent@latest
```

### 2. Make the options available to your protos

Your `.proto` files need to import `fluent/v1/options.proto`. Pick one:

**a) Let the plugin write it (recommended).** This needs no registry and no network:

```sh
protoc-gen-go-fluent init proto       # or: go tool protoc-gen-go-fluent init proto
# created proto/fluent/v1/options.proto
```

The file is embedded in the plugin binary, so it always matches the plugin version.
Re-run `init` after upgrading the plugin. It prints `unchanged` when the file is
already current. Commit the file, and generate Go code for it like any other proto:
with managed mode (`go_package_prefix`) it ends up in your own `gen/` tree, and your
generated code never imports this module's `fluentpb` package.

**b) Use the Buf Schema Registry**, if the module is published there:

```yaml
# buf.yaml
version: v2
modules:
  - path: proto
deps:
  - buf.build/wakelesstuna/fluent
```

### 3. Annotate your services

```proto
syntax = "proto3";

package library.v1;

import "fluent/v1/options.proto";

// The resource tree: Client.Shelf(shelfId).Book(bookId)
option (fluent.v1.resource) = { name: "Shelf" id_field: "shelf_id" };
option (fluent.v1.resource) = { name: "Book" parent: "Shelf" id_field: "book_id" };

service LibraryService {
  rpc ListShelves(ListShelvesRequest) returns (ListShelvesResponse) {
    option (fluent.v1.list) = {};                     // Client.Shelves()
  }
  rpc ListBooks(ListBooksRequest) returns (ListBooksResponse) {
    option (fluent.v1.list) = { parent: "Shelf" };    // ShelfRef.Books()
  }
  rpc ListReviews(ListReviewsRequest) returns (ListReviewsResponse) {
    option (fluent.v1.list) = { parent: "Book" };     // BookRef.Reviews()
  }
}

message ListBooksRequest {
  string shelf_id = 1;          // filled in from ShelfRef
  int32 page_size = 2;          // paging, handled for you
  string page_token = 3;
  string author = 4;            // -> .Author(string)
  string title_contains = 5 [(fluent.v1.field).setter = "Title"];      // -> .Title(string)
  Genre genre = 6 [(fluent.v1.field).enum_shortcut = "Only%s"];        // -> .Genre(v), .OnlyFiction(), ...
  bool debug = 7 [(fluent.v1.field).skip = true];                      // no setter
}
```

The complete, compiling version is in [`example/`](example/).

### 4. Configure generation

```yaml
# buf.gen.yaml
version: v2
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: example.com/myapi/gen     # options.pb.go lands in gen/fluent/v1 too
plugins:
  - local: protoc-gen-go
    out: gen
    opt: paths=source_relative
  - local: protoc-gen-go-grpc
    out: gen
    opt: paths=source_relative
  - local: protoc-gen-go-fluent         # or ["go", "tool", "protoc-gen-go-fluent"]
    out: client
    strategy: all                       # required: resources may span files
    opt:
      - import_path=example.com/myapi/client
```

If you don't use managed mode, your files need their own `go_package`. Edit the one in
`fluent/v1/options.proto` to point into your module (re-apply it after each `init`), or
leave it as is to import this module's `fluentpb` package.

If you get the options from the Buf Schema Registry instead (option b), add a `disable`
rule so managed mode keeps the published `go_package`:

```yaml
  disable:
    - file_option: go_package_prefix
      module: buf.build/wakelesstuna/fluent
```

Run it:

```sh
buf generate
go get github.com/wakelesstuna/protoc-gen-go-fluent   # the generated code imports the fluent runtime
```

### 5. Use the client

```go
c, err := client.Dial("library.example.com:443",
	grpc.WithTransportCredentials(credentials.NewTLS(nil)))
if err != nil { ... }
defer c.Close()

// Per request: add the caller's credentials. Cheap; shares the connection.
user := c.With(fluent.Metadata("authorization", "Bearer "+token))

for shelf, err := range user.Shelves().All(ctx) { ... }

books, err := user.Shelf("sci-fi").Books().Author("Le Guin").Limit(20).Collect(ctx)

page, err := user.Shelf("sci-fi").Book("b1").Reviews().PageSize(10).Page(ctx, pageToken)
```

## Annotation reference

All options are defined in [`proto/fluent/v1/options.proto`](proto/fluent/v1/options.proto).

### `(fluent.v1.resource)` (file option, repeatable)

Declares a node in the navigation tree.

| Field      | Meaning |
|------------|---------|
| `name`     | Go name, e.g. `Book`. Generates a `BookRef` type and a `Book(id string)` method. |
| `parent`   | Name of the parent resource. Empty means the method goes on `Client`. |
| `id_field` | Request field holding this resource's id, e.g. `book_id`. Every list request under this resource must have it as a `string`. |

Resources can be declared in any file of the generation run (hence `strategy: all`).

### `(fluent.v1.list)` (method option)

Generates a query builder for a unary list RPC.

| Field    | Meaning |
|----------|---------|
| `parent` | Resource the query hangs off. Empty means `Client`. |
| `name`   | Method name. Defaults to the RPC name without `List` (`ListBooks` → `Books`). |

How the request and response fields are used:
- **Id fields** of the parent chain are filled in from the Ref.
- **`page_size` and `page_token`** drive paging.
- **Every other request field** becomes a setter.
- **The response's one repeated message field** is the item type.

### `(fluent.v1.field)` (field option)

| Field           | Meaning |
|-----------------|---------|
| `setter`        | Rename the setter (`title_contains` → `Title`). |
| `enum_shortcut` | For enums: also generate one no-argument method per non-zero value. `%s` is the value name in CamelCase without the enum prefix: `"Only%s"` turns `GENRE_SCIENCE_FICTION` into `OnlyScienceFiction()`. |
| `skip`          | Don't generate a setter. Required for field types setters don't support. |

## Generated API

For each list RPC you get a `<Name>Query` type with:

| Method                    | What it does |
|---------------------------|--------------|
| `<Setter>(v)`             | One per request field. Returns a modified copy. |
| `PageSize(n)`             | Items per request. 0 uses the server default. |
| `Limit(n)`                | Cap on the total items `All`/`Collect` return. |
| `Page(ctx, token)`        | One page: `fluent.Page[T]{Items, NextPageToken}`. Pass `""` for the first page. |
| `All(ctx)`                | `iter.Seq2[T, error]` over every item. Pages are fetched lazily, and `break` stops fetching. |
| `Collect(ctx)`            | `All` gathered into a slice. |

Queries are **immutable values**, so you can build a base query once and branch from it:

```go
sciFi := c.Shelf("sci-fi").Books().OnlyScienceFiction()
leGuin, _ := sciFi.Author("Le Guin").Collect(ctx)
butler, _ := sciFi.Author("Octavia Butler").Collect(ctx)   // unaffected by the line above
```

Errors are standard gRPC status errors (`status.Code(err)`).

With the default `client=true`, you also get:

| Function                     | What it does |
|------------------------------|--------------|
| `Dial(target, opts...)`      | Opens a connection. You must pass transport credentials. |
| `New(cc)`                    | Wraps an existing `grpc.ClientConnInterface`, which you keep owning. |
| `(*Client).With(fns...)`     | Derives a client that applies `fluent.ContextFunc`s to every call. |
| `(*Client).Close()`          | Closes the connection if `Dial` opened it. Closing a derived client is a no-op. |

## Custom clients (`client=false`)

Set `client=false` when you want your own constructor and options, for example to keep
TLS on by default, or to add helpers like `AsUser(user)`. The generated code then
expects you to declare `Client` in the same package, embedding `*fluent.Conn`:

```go
package myclient

type Client struct {
	*fluent.Conn
}

func Dial(target string) (*Client, error) {
	conn, err := fluent.Dial(target, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	if err != nil {
		return nil, err
	}
	return &Client{Conn: conn}, nil
}

// AsUser adds a per-call token. Token() is called on every request, so it can refresh.
func (c *Client) AsUser(u interface{ Token() string }) *Client {
	return &Client{Conn: c.Conn.With(func(ctx context.Context) context.Context {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+u.Token())
	})}
}
```

Your `Client` must not define methods with the same names as generated
resources or queries.

## Plugin options

| Option        | Default          | Meaning |
|---------------|------------------|---------|
| `import_path` | *(required)*     | Go import path of the output package. |
| `package`     | last path element | Go package name. |
| `filename`    | `fluent.gen.go`  | Output file name. |
| `client`      | `true`           | Generate `Client`, `Dial`, `New` and `With`. |

Run directly (not from buf/protoc), the binary also has these commands:

| Command                                   | What it does |
|-------------------------------------------|--------------|
| `protoc-gen-go-fluent init [proto-root]`  | Writes `fluent/v1/options.proto` under `proto-root` (default `proto`). |
| `protoc-gen-go-fluent --version`          | Prints the version. |

## Runtime package

The generated code imports [`github.com/wakelesstuna/protoc-gen-go-fluent/fluent`](fluent/):

- `fluent.Conn` holds the shared connection and per-call context functions. Its methods
  are `With`, `Outgoing`, `ClientConn` and `Close`.
- `fluent.ContextFunc` and `fluent.Metadata(kv...)` decorate outgoing calls.
- `fluent.Page`, `fluent.Paging`, `fluent.All` and `fluent.Collect` are the pagination
  engine. You can use them on their own for hand-written paginated calls too.

## Validation

The plugin fails generation with a specific message rather than emitting broken code.
It checks for:
- unknown parent resources, and parent cycles
- missing or wrongly typed paging fields or id fields
- responses without exactly one repeated message field
- name collisions between setters, generated methods and `Client` methods
- invalid `enum_shortcut` patterns
- unsupported setter types (repeated, map, message, `optional`, oneof), which you mark `skip`
- streaming RPCs

## Limitations

- Only unary list RPCs get query builders. Get/Create/Update/Delete calls go through the
  regular gRPC stubs.
- Setters cover scalar and enum fields. Message, repeated, map and `optional` fields need `skip`.
- The receiver type is always named `Client`.

## Developing

```sh
buf generate      # regenerates fluentpb and the example client (runs the plugin from source)
go test ./...
```

`fluentpb` is checked in because the plugin itself depends on it. If you change
`options.proto` in a way that breaks the build, regenerate with the Go plugins only first:

```sh
buf generate --template '{"version":"v2","plugins":[{"local":"protoc-gen-go","out":".","opt":"module=github.com/wakelesstuna/protoc-gen-go-fluent"}]}'
```

### Releasing

Tag a semver version. `go install ...@latest` and `go get` pick it up from the tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

To publish the options to the Buf Schema Registry, create the `wakelesstuna/fluent`
repository on buf.build and run `buf push proto`.

## License

[MIT](LICENSE)
