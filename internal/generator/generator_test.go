package generator

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	libraryv1 "github.com/wakelesstuna/protoc-gen-go-fluent/example/gen/library/v1"
	"github.com/wakelesstuna/protoc-gen-go-fluent/fluentpb"
)

// The descriptor embedded in the example's generated code carries the fluent
// options (but no comments), which is enough to exercise generation and validation.
func generate(t *testing.T, cfg Config, mutate func(f *descriptorpb.FileDescriptorProto)) (string, error) {
	t.Helper()
	deps := []protoreflect.FileDescriptor{
		descriptorpb.File_google_protobuf_descriptor_proto,
		fluentpb.File_fluent_v1_options_proto,
	}
	var files []*descriptorpb.FileDescriptorProto
	for _, d := range deps {
		files = append(files, protodesc.ToFileDescriptorProto(d))
	}
	lib := protodesc.ToFileDescriptorProto(libraryv1.File_library_v1_library_proto)
	if mutate != nil {
		mutate(lib)
	}
	files = append(files, lib)

	p, err := protogen.Options{}.New(&pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{lib.GetName()},
		ProtoFile:      files,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PackageName == "" {
		cfg = Config{PackageName: "client", ImportPath: "example.com/client", Filename: "fluent.gen.go", Client: cfg.Client}
	}
	if err := Generate(p, cfg); err != nil {
		return "", err
	}
	resp := p.Response()
	if resp.Error != nil {
		t.Fatal(resp.GetError())
	}
	if len(resp.File) != 1 {
		t.Fatalf("generated %d files, want 1", len(resp.File))
	}
	return resp.File[0].GetContent(), nil
}

func method(f *descriptorpb.FileDescriptorProto, name string) *descriptorpb.MethodDescriptorProto {
	for _, s := range f.Service {
		for _, m := range s.Method {
			if m.GetName() == name {
				return m
			}
		}
	}
	panic("no method " + name)
}

func message(f *descriptorpb.FileDescriptorProto, name string) *descriptorpb.DescriptorProto {
	for _, m := range f.MessageType {
		if m.GetName() == name {
			return m
		}
	}
	panic("no message " + name)
}

func field(m *descriptorpb.DescriptorProto, name string) *descriptorpb.FieldDescriptorProto {
	for _, f := range m.Field {
		if f.GetName() == name {
			return f
		}
	}
	panic("no field " + name)
}

func removeField(m *descriptorpb.DescriptorProto, name string) {
	for i, f := range m.Field {
		if f.GetName() == name {
			m.Field = append(m.Field[:i], m.Field[i+1:]...)
			return
		}
	}
	panic("no field " + name)
}

func setFieldOption(f *descriptorpb.FieldDescriptorProto, opt *fluentpb.Field) {
	if f.Options == nil {
		f.Options = &descriptorpb.FieldOptions{}
	}
	proto.SetExtension(f.Options, fluentpb.E_Field, opt)
}

func TestGenerate(t *testing.T) {
	out, err := generate(t, Config{Client: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type Client struct {\n\t*fluent.Conn\n}",
		"func Dial(target string, opts ...grpc.DialOption) (*Client, error)",
		"func (c *Client) With(fns ...fluent.ContextFunc) *Client",
		"func (c *Client) Shelf(shelfId string) ShelfRef",
		"func (r ShelfRef) Book(bookId string) BookRef",
		"func (c *Client) Shelves() ShelvesQuery",
		"func (r BookRef) Reviews() ReviewsQuery",
		"func (q BooksQuery) Title(v string) BooksQuery",
		"func (q BooksQuery) OnlyScienceFiction() BooksQuery",
		"q.genre = v1.Genre_GENRE_SCIENCE_FICTION",
		"func (q ReviewsQuery) MinStars(v int32) ReviewsQuery",
		"stub := v1.NewLibraryServiceClient(q.c.Conn.ClientConn())",
		"Items: resp.GetReviews(), NextPageToken: resp.GetNextPageToken()",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated code missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"func (q ReviewsQuery) ShelfId(", // id fields come from the Ref
		"func (q BooksQuery) Debug(",     // skipped
		"func (q BooksQuery) TitleContains(",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("generated code unexpectedly contains %q", unwanted)
		}
	}
}

func TestGenerateWithoutClient(t *testing.T) {
	out, err := generate(t, Config{Client: false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "type Client struct") || strings.Contains(out, "func Dial(") {
		t.Error("client=false still generated the Client type")
	}
	if !strings.Contains(out, "func (c *Client) Shelves() ShelvesQuery") {
		t.Error("client=false dropped the query methods")
	}
}

func TestGenerateErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(f *descriptorpb.FileDescriptorProto)
		wantErr string
	}{
		{
			name: "unknown list parent",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				proto.SetExtension(method(f, "ListBooks").Options, fluentpb.E_List, &fluentpb.ListMethod{Parent: "Nope"})
			},
			wantErr: `unknown parent resource "Nope"`,
		},
		{
			name: "missing paging field",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				removeField(message(f, "ListBooksRequest"), "page_token")
			},
			wantErr: "ListBooksRequest is missing field page_token",
		},
		{
			name: "missing next_page_token",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				removeField(message(f, "ListBooksResponse"), "next_page_token")
			},
			wantErr: "ListBooksResponse is missing field next_page_token",
		},
		{
			name: "missing ancestor id field",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				proto.SetExtension(method(f, "ListBooks").Options, fluentpb.E_List, &fluentpb.ListMethod{Parent: "Book"})
			},
			wantErr: "ListBooksRequest is missing field book_id (id_field of resource Book)",
		},
		{
			name: "setter collides with terminal method",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				setFieldOption(field(message(f, "ListBooksRequest"), "author"), &fluentpb.Field{Setter: "Limit"})
			},
			wantErr: "setter Limit collides with BooksQuery.Limit",
		},
		{
			name: "two setters with one name",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				setFieldOption(field(message(f, "ListBooksRequest"), "author"), &fluentpb.Field{Setter: "Title"})
			},
			wantErr: "both generate BooksQuery.Title",
		},
		{
			name: "enum shortcut on string field",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				setFieldOption(field(message(f, "ListBooksRequest"), "author"), &fluentpb.Field{EnumShortcut: "%sAuthor"})
			},
			wantErr: "enum_shortcut needs an enum field",
		},
		{
			name: "bad enum shortcut pattern",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				setFieldOption(field(message(f, "ListBooksRequest"), "genre"), &fluentpb.Field{EnumShortcut: "Only"})
			},
			wantErr: "must contain exactly one %s",
		},
		{
			name: "resource parent cycle",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				proto.SetExtension(f.Options, fluentpb.E_Resource, []*fluentpb.Resource{
					{Name: "Shelf", Parent: "Book", IdField: "shelf_id"},
					{Name: "Book", Parent: "Shelf", IdField: "book_id"},
				})
			},
			wantErr: "parent cycle",
		},
		{
			name: "resource collides with Client method",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				proto.SetExtension(f.Options, fluentpb.E_Resource, []*fluentpb.Resource{
					{Name: "Close", IdField: "shelf_id"},
				})
				proto.ClearExtension(method(f, "ListBooks").Options, fluentpb.E_List)
				proto.ClearExtension(method(f, "ListReviews").Options, fluentpb.E_List)
			},
			wantErr: "resource Close collides with Client.Close",
		},
		{
			name: "unsupported setter type",
			mutate: func(f *descriptorpb.FileDescriptorProto) {
				field(message(f, "ListBooksRequest"), "author").Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
			},
			wantErr: "repeated and map fields are not supported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := generate(t, Config{Client: true}, tt.mutate)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestQueryNameDefaults(t *testing.T) {
	// An RPC named just "List" takes its name from the response's items field.
	out, err := generate(t, Config{Client: true}, func(f *descriptorpb.FileDescriptorProto) {
		method(f, "ListBooks").Name = proto.String("List")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "func (r ShelfRef) Books() BooksQuery") {
		t.Error(`RPC "List" did not fall back to the items field name`)
	}
	if !strings.Contains(out, "resp, err := stub.List(") {
		t.Error("query does not call the List RPC")
	}

	// An explicit name wins.
	out, err = generate(t, Config{Client: true}, func(f *descriptorpb.FileDescriptorProto) {
		proto.SetExtension(method(f, "ListBooks").Options, fluentpb.E_List, &fluentpb.ListMethod{Parent: "Shelf", Name: "Catalog"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "func (r ShelfRef) Catalog() CatalogQuery") {
		t.Error("explicit name ignored")
	}
}

func TestInvalidQueryNameExplainsFix(t *testing.T) {
	_, err := generate(t, Config{Client: true}, func(f *descriptorpb.FileDescriptorProto) {
		proto.SetExtension(method(f, "ListBooks").Options, fluentpb.E_List, &fluentpb.ListMethod{Parent: "Shelf", Name: "books"})
	})
	if err == nil || !strings.Contains(err.Error(), "library.v1.LibraryService.ListBooks") || !strings.Contains(err.Error(), "(fluent.v1.list) = {name:") {
		t.Errorf("err = %v", err)
	}
}

func TestNaming(t *testing.T) {
	for in, want := range map[string]string{
		"BranchOrder": "BRANCH_ORDER",
		"HTTPMethod":  "HTTP_METHOD",
		"V2Thing":     "V2_THING",
		"Genre":       "GENRE",
	} {
		if got := upperSnake(in); got != want {
			t.Errorf("upperSnake(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"shelf_id":        "ShelfId",
		"SCIENCE_FICTION": "ScienceFiction",
		"FICTION":         "Fiction",
	} {
		if got := camel(in); got != want {
			t.Errorf("camel(%q) = %q, want %q", in, got, want)
		}
	}
}
